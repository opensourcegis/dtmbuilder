package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui.html
var ui string

//go:embed app.js
var appJS string

//go:embed editor.js
var editorJS string

//go:embed style.css
var styleCSS string

//go:embed contours.js
var contoursJS string

type studio struct {
	sync.Mutex
	grid         *Grid
	result       *Result
	name         string
	busy         bool
	progress     int
	message      string
	failure      string
	cancel       context.CancelFunc
	params       Params
	token, host  string
	desktop      bool
	edits        editHistory
	resultMethod string
	contours     *ContourSet
}

func main() {
	port := flag.Int("port", 8787, "local GUI port (0 selects a free port)")
	open := flag.Bool("open", true, "open browser")
	browserMode := flag.Bool("browser", false, "use the optional local browser interface instead of the desktop window")
	input := flag.String("input", "", "DSM path for command-line processing")
	output := flag.String("output", "outputs", "CLI output folder")
	config := flag.String("config", "", "JSON parameter file for CLI")
	flag.Parse()
	if *input != "" {
		p := defaults()
		if *config != "" {
			b, e := os.ReadFile(*config)
			if e != nil {
				log.Fatal(e)
			}
			if e = json.Unmarshal(b, &p); e != nil {
				log.Fatal(e)
			}
		}
		g, e := readTIFF(*input)
		if e != nil {
			log.Fatal(e)
		}
		r, e := process(context.Background(), g, p, func(n int, s string) { log.Printf("%d%% %s", n, s) })
		if e != nil {
			log.Fatal(e)
		}
		if e = os.MkdirAll(*output, 0755); e != nil {
			log.Fatal(e)
		}
		for name, z := range map[string][]float32{"dtm": r.DTM, "cleaned": r.Clean, "ndsm": r.NDSM, "flags": r.Flags} {
			b, e := encodeTIFF(g, z)
			if e != nil {
				log.Fatal(e)
			}
			dest := filepath.Join(*output, name+".tif")
			f, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if e != nil {
				log.Fatal(e)
			}
			_, e = f.Write(b)
			closeErr := f.Close()
			if e != nil {
				log.Fatal(e)
			}
			if closeErr != nil {
				log.Fatal(closeErr)
			}
		}
		report, _ := json.MarshalIndent(map[string]any{"parameters": p, "counts": r.Counts, "input": *input}, "", "  ")
		if e = os.WriteFile(filepath.Join(*output, "report.json"), report, 0644); e != nil {
			log.Fatal(e)
		}
		log.Printf("Complete: %s", *output)
		return
	}
	if !*browserMode {
		secret := make([]byte, 24)
		if _, e := rand.Read(secret); e != nil {
			log.Fatal(e)
		}
		s := &studio{desktop: true, params: defaults(), message: "Load an elevation DSM or try the demonstration", token: hex.EncodeToString(secret)}
		if e := runDesktop(s); e != nil {
			log.Fatal(e)
		}
		return
	}
	listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if e != nil {
		host := fmt.Sprintf("127.0.0.1:%d", *port)
		if existingStudio(host) {
			url := "http://" + host
			log.Printf("Terrain Studio is already running: %s", url)
			if *open {
				if err := openBrowser(url); err != nil {
					log.Printf("Open this address in your browser: %s (%v)", url, err)
				}
			}
			return
		}
		log.Printf("Port %d unavailable; selecting a free local port", *port)
		listener, e = net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			log.Fatal(e)
		}
	}
	secret := make([]byte, 24)
	if _, e = rand.Read(secret); e != nil {
		log.Fatal(e)
	}
	s := &studio{params: defaults(), message: "Load an elevation DSM or try the demonstration", token: hex.EncodeToString(secret), host: listener.Addr().String()}
	url := "http://" + s.host
	log.Printf("Terrain Studio Go: %s", url)
	if *open {
		go func() {
			time.Sleep(300 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				log.Printf("Browser launch failed: %v. Open %s manually.", err, url)
			}
		}()
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.Serve(listener))
}

func existingStudio(host string) bool {
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	r, err := client.Get("http://" + host + "/api/health")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 100))
	return err == nil && r.StatusCode == http.StatusOK && string(b) == "terrain-studio-go"
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		// The address is generated exclusively from a loopback listener and numeric port.
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", "Start-Process -FilePath '"+url+"'").Run()
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *studio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	if !s.desktop && r.Host != s.host {
		http.Error(w, "invalid host", 403)
		return
	}
	if o := r.Header.Get("Origin"); !s.desktop && o != "" && o != "http://"+s.host {
		http.Error(w, "invalid origin", 403)
		return
	}
	if r.URL.Path == "/" && r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := strings.ReplaceAll(ui, "__TOKEN__", s.token)
		page = strings.ReplaceAll(page, "__DESKTOP__", fmt.Sprint(s.desktop))
		fmt.Fprint(w, page)
		return
	}
	if r.Method == "GET" {
		assets := map[string]struct{ body, kind string }{"/app.js": {appJS, "text/javascript"}, "/editor.js": {editorJS, "text/javascript"}, "/style.css": {styleCSS, "text/css"}, "/contours.js": {contoursJS, "text/javascript"}}
		if a, ok := assets[r.URL.Path]; ok {
			w.Header().Set("Content-Type", a.kind+"; charset=utf-8")
			fmt.Fprint(w, a.body)
			return
		}
	}
	if r.URL.Path == "/api/health" && r.Method == "GET" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "terrain-studio-go")
		return
	}
	if r.Header.Get("X-Studio-Token") != s.token {
		http.Error(w, "local session token required", 403)
		return
	}
	switch {
	case r.URL.Path == "/api/state" && r.Method == "GET":
		s.Lock()
		defer s.Unlock()
		reply(w, s.state())
	case r.URL.Path == "/api/demo" && r.Method == "POST":
		s.Lock()
		defer s.Unlock()
		if s.busy {
			http.Error(w, "job running", 409)
			return
		}
		s.grid = demo()
		s.name = "Synthetic terrain · buildings, trees & outliers"
		s.result = nil
		s.resetEdits()
		s.failure = ""
		s.progress = 0
		s.message = "Demonstration loaded"
		reply(w, s.state())
	case r.URL.Path == "/api/upload" && r.Method == "POST":
		s.Lock()
		defer s.Unlock()
		if s.busy {
			http.Error(w, "job running", 409)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
		tmp, e := os.CreateTemp("", "terrain-*.tif")
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		path := tmp.Name()
		defer os.Remove(path)
		_, e = tmp.ReadFrom(r.Body)
		closeErr := tmp.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		g, e := readTIFF(path)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		s.grid = g
		s.name = filepath.Base(r.Header.Get("X-File-Name"))
		s.result = nil
		s.resetEdits()
		if r.URL.Query().Get("mode") == "dtm" {
			s.result = initializeDTM(g)
			s.params = defaults()
			s.resultMethod = "Imported DTM"
		}
		s.failure = ""
		s.progress = 0
		s.message = "Elevation raster loaded"
		reply(w, s.state())
	case r.URL.Path == "/api/run" && r.Method == "POST":
		var p Params
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&p); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if e := p.validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		s.Lock()
		if s.busy || s.grid == nil {
			s.Unlock()
			http.Error(w, "load a raster and wait for any active job", 409)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.busy = true
		s.failure = ""
		s.result = nil
		s.resetEdits()
		s.params = p
		s.progress = 0
		s.message = "Starting"
		g := s.grid
		s.Unlock()
		go func() {
			defer cancel()
			out, e := process(ctx, g, p, func(n int, m string) { s.Lock(); s.progress = n; s.message = m; s.Unlock() })
			s.Lock()
			defer s.Unlock()
			s.busy = false
			if e != nil {
				s.failure = e.Error()
				s.message = "Processing stopped"
				return
			}
			s.result = out
			s.resultMethod = "Progressive morphological opening with constrained smoothing"
			s.progress = 100
			s.message = "Processing complete"
		}()
		reply(w, map[string]bool{"started": true})
	case r.URL.Path == "/api/contours/generate" && r.Method == "POST":
		var p ContourParams
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if e := d.Decode(&p); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if e := p.validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		s.Lock()
		if s.busy || s.result == nil {
			s.Unlock()
			http.Error(w, "create/open a DTM and wait for the active job", 409)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.busy = true
		s.failure = ""
		s.progress = 0
		s.message = "Generating DTM contours"
		g, z, revision := s.grid, s.result.DTM, s.edits.Revision
		s.Unlock()
		go func() {
			defer cancel()
			c, e := generateContours(ctx, g, z, p, revision, func(n int, m string) { s.Lock(); s.progress = n; s.message = m; s.Unlock() })
			s.Lock()
			defer s.Unlock()
			s.busy = false
			if e != nil {
				s.failure = e.Error()
				s.message = "Contour generation stopped"
				return
			}
			s.contours = c
			s.progress = 100
			s.message = fmt.Sprintf("Contours ready: %d lines", len(c.Lines))
		}()
		reply(w, map[string]bool{"started": true})
	case r.URL.Path == "/api/contours" && r.Method == "GET":
		s.Lock()
		c := s.contours
		s.Unlock()
		if c == nil {
			http.Error(w, "contours not generated", 404)
			return
		}
		reply(w, c)
	case r.URL.Path == "/api/contours/export" && r.Method == "GET":
		s.Lock()
		g, c := s.grid, s.contours
		s.Unlock()
		if c == nil {
			http.Error(w, "generate contours first", 409)
			return
		}
		b, e := contourCSV(g, c)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="contours.csv"`)
		_, _ = w.Write(b)
	case r.URL.Path == "/api/contours/surface" && r.Method == "GET":
		s.Lock()
		c := s.contours
		s.Unlock()
		if c == nil {
			http.Error(w, "generate contours first", 409)
			return
		}
		b, e := encodeTIFF(c.Surface, c.Surface.Z)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/tiff")
		w.Header().Set("Content-Disposition", `attachment; filename="contour-surface.tif"`)
		_, _ = w.Write(b)
	case r.URL.Path == "/api/edit" && r.Method == "POST":
		var q EditRequest
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if e := dec.Decode(&q); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		s.Lock()
		defer s.Unlock()
		if e := s.applyEdit(q); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		reply(w, s.state())
	case (r.URL.Path == "/api/undo" || r.URL.Path == "/api/redo") && r.Method == "POST":
		s.Lock()
		defer s.Unlock()
		if e := s.historyEdit(r.URL.Path == "/api/redo"); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		reply(w, s.state())
	case r.URL.Path == "/api/edit-base" && r.Method == "POST":
		s.Lock()
		defer s.Unlock()
		if s.busy || s.grid == nil || s.result != nil {
			http.Error(w, "load a raster with no active result first", 409)
			return
		}
		s.result = initializeDTM(s.grid)
		s.resultMethod = "Input raster used directly as DTM"
		s.params = defaults()
		s.resetEdits()
		s.message = "Input DTM ready for editing"
		reply(w, s.state())
	case r.URL.Path == "/api/inspect" && r.Method == "GET":
		x, e1 := strconv.Atoi(r.URL.Query().Get("x"))
		y, e2 := strconv.Atoi(r.URL.Query().Get("y"))
		if e1 != nil || e2 != nil {
			http.Error(w, "invalid source cell", 400)
			return
		}
		s.Lock()
		defer s.Unlock()
		v, e := s.inspect(x, y)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		reply(w, v)
	case r.URL.Path == "/api/cancel" && r.Method == "POST":
		s.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.Unlock()
		reply(w, map[string]bool{"cancelled": true})
	case r.URL.Path == "/api/preview" && r.Method == "GET":
		s.Lock()
		if r.URL.Query().Get("layer") == "contour-surface" {
			g, c := s.grid, s.contours
			s.Unlock()
			if g == nil || c == nil {
				http.Error(w, "generate contours first", 404)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_ = png.Encode(w, previewContourSurface(g, c))
			return
		}
		g, z := s.layer(r.URL.Query().Get("layer"))
		s.Unlock()
		if g == nil || z == nil {
			http.Error(w, "layer unavailable", 404)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, preview(g, z, r.URL.Query().Get("layer")))
	case r.URL.Path == "/api/download" && r.Method == "GET":
		s.Lock()
		g, z := s.layer(r.URL.Query().Get("layer"))
		s.Unlock()
		if g == nil || z == nil {
			http.Error(w, "layer unavailable", 404)
			return
		}
		b, e := encodeTIFF(g, z)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/tiff")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeLayer(r.URL.Query().Get("layer"))+`.tif"`)
		_, _ = w.Write(b)
	case r.URL.Path == "/api/report" && r.Method == "GET":
		s.Lock()
		defer s.Unlock()
		if s.result == nil {
			http.Error(w, "run processing first", 409)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="terrain-report.json"`)
		reply(w, map[string]any{"input": s.name, "width": s.grid.W, "height": s.grid.H, "parameters": s.params, "counts": s.result.Counts, "edits": s.edits.Audit, "method": s.resultMethod, "flags": "1 low, 2 high, 4 spike, 8 non-ground, 16 original NoData, 32 filled output, 64 manual edit, 128 hydroflatten; bitwise combined", "fill_original_nodata_in_outputs": s.params.FillOutput})
	default:
		http.NotFound(w, r)
	}
}
func safeLayer(k string) string {
	switch k {
	case "dsm", "cleaned", "dtm", "ndsm", "flags":
		return k
	}
	return "dtm"
}
func (s *studio) layer(k string) (*Grid, []float32) {
	if s.grid == nil {
		return nil, nil
	}
	if k == "dsm" {
		return s.grid, s.grid.Z
	}
	if s.result == nil {
		return s.grid, nil
	}
	switch k {
	case "cleaned":
		return s.grid, s.result.Clean
	case "dtm":
		return s.grid, s.result.DTM
	case "ndsm":
		return s.grid, s.result.NDSM
	case "flags":
		return s.grid, s.result.Flags
	}
	return nil, nil
}
func (s *studio) state() map[string]any {
	m := map[string]any{"busy": s.busy, "progress": s.progress, "message": s.message, "error": s.failure, "ready": s.grid != nil, "complete": s.result != nil, "defaults": defaults()}
	m["editing"] = map[string]any{"revision": s.edits.Revision, "undo": len(s.edits.Undo), "redo": len(s.edits.Redo), "audit": s.edits.Audit}
	m["unsavedEdits"] = s.result != nil && s.result.Counts["manual"] > 0 && s.edits.Revision != s.edits.SavedRevision
	if s.contours != nil {
		m["contours"] = map[string]any{"revision": s.contours.Revision, "parameters": s.contours.Params, "lines": len(s.contours.Lines), "segments": s.contours.Segments, "removedSmallLoops": s.contours.RemovedSmallLoops}
	} else {
		m["contours"] = nil
	}
	if s.grid != nil {
		lo, hi := extent(s.grid.Z)
		m["raster"] = map[string]any{"name": s.name, "width": s.grid.W, "height": s.grid.H, "min": lo, "max": hi, "pixelSize": s.grid.PixelSize, "georeferenced": len(s.grid.Tags[34735].Data) > 0}
		m["raster"].(map[string]any)["horizontalUnit"] = horizontalUnit(s.grid)
	}
	if s.result != nil {
		m["counts"] = s.result.Counts
	}
	return m
}
func extent(z []float32) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range z {
		if valid(v) {
			lo = math.Min(lo, float64(v))
			hi = math.Max(hi, float64(v))
		}
	}
	if math.IsInf(lo, 0) {
		return 0, 0
	}
	return lo, hi
}
func preview(g *Grid, z []float32, layer string) image.Image {
	scale := math.Max(1, math.Max(float64(g.W), float64(g.H))/1000)
	w, h := max(1, int(float64(g.W)/scale)), max(1, int(float64(g.H)/scale))
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	basis := hillshadeBasis(g)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			xx, yy := min(g.W-1, int(float64(x)*scale)), min(g.H-1, int(float64(y)*scale))
			v := z[yy*g.W+xx]
			c := color.NRGBA{0, 0, 0, 0}
			if valid(v) {
				if layer == "flags" {
					switch {
					case int(v)&128 != 0:
						c = color.NRGBA{70, 153, 230, 255}
					case int(v)&64 != 0:
						c = color.NRGBA{45, 186, 112, 255}
					case int(v)&1 != 0:
						c = color.NRGBA{85, 156, 255, 255}
					case int(v)&2 != 0:
						c = color.NRGBA{255, 113, 92, 255}
					case int(v)&4 != 0:
						c = color.NRGBA{255, 203, 96, 255}
					case int(v)&32 != 0:
						c = color.NRGBA{45, 203, 188, 255}
					case int(v)&16 != 0:
						c = color.NRGBA{20, 31, 43, 255}
					case int(v)&8 != 0:
						c = color.NRGBA{173, 136, 245, 255}
					default:
						c = color.NRGBA{48, 68, 76, 255}
					}
				} else {
					shade := hillshadeAt(g, z, xx, yy, basis)
					c = color.NRGBA{shade, shade, shade, 255}
				}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	return im
}
func demo() *Grid {
	w, h := 256, 256
	z := make([]float32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 50 + float64(x)*.025 + 2*math.Sin(float64(y)/60) + 1.5*math.Sin(float64(x)/45)
			if x > 45 && x < 80 && y > 50 && y < 88 {
				v += 12
			}
			if x > 140 && x < 179 && y > 135 && y < 168 {
				v += 8
			}
			dx, dy := float64(x-175), float64(y-64)
			if dx*dx+dy*dy < 400 {
				v += 6 * math.Sqrt(1-(dx*dx+dy*dy)/400)
			}
			z[y*w+x] = float32(v)
		}
	}
	z[120*w+80] -= 25
	z[115*w+90] += 30
	z[110*w+100] += 2
	for y := 200; y < 204; y++ {
		for x := 40; x < 44; x++ {
			z[y*w+x] = float32(math.NaN())
		}
	}
	return &Grid{W: w, H: h, Z: z, Tags: map[uint16]tag{}}
}
