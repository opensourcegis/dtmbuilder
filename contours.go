package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

type ContourParams struct {
	Interval       float64 `json:"interval"`
	Base           float64 `json:"base"`
	IndexEvery     int     `json:"indexEvery"`
	AverageFactor  int     `json:"averageFactor"`
	SmoothRadius   int     `json:"smoothRadius"`
	RegionalRadius int     `json:"regionalRadius"`
	MinLength      float64 `json:"minLength"`
	MinArea        float64 `json:"minArea"`
}

func defaultContourParams() ContourParams {
	return ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, MinArea: 50}
}
func (p ContourParams) validate() error {
	if !validNumber(p.Interval) || p.Interval < .0001 || !validNumber(p.Base) || math.Abs(p.Base) > 1e8 || p.IndexEvery < 1 || p.IndexEvery > 100 || p.AverageFactor < 1 || p.AverageFactor > 100 || p.SmoothRadius < 0 || p.SmoothRadius > 5 || p.RegionalRadius < 0 || p.RegionalRadius > 100 || !validNumber(p.MinLength) || p.MinLength < 0 || !validNumber(p.MinArea) || p.MinArea < 0 {
		return errors.New("invalid contour settings: interval ≥0.0001, index 1–100, averaging 1–100, smoothing radius 0–5")
	}
	return nil
}

type ContourLine struct {
	Elevation float64     `json:"elevation"`
	Index     bool        `json:"index"`
	Points    []CellPoint `json:"points"`
	AreaM2    *float64    `json:"areaM2,omitempty"`
}
type ContourSet struct {
	Params            ContourParams `json:"parameters"`
	Revision          int           `json:"revision"`
	Lines             []ContourLine `json:"lines"`
	Surface           *Grid         `json:"-"`
	Segments          int           `json:"segments"`
	RemovedSmallLoops int           `json:"removedSmallLoops"`
}
type contourNodeKey struct {
	level int64
	edge  int
}
type contourNode struct {
	point    CellPoint
	segments []int
}
type contourSegment struct{ a, b int }

func contourSurface(ctx context.Context, g *Grid, z []float32, p ContourParams) (*Grid, error) {
	f := p.AverageFactor
	w, h := g.W/f, g.H/f
	if w < 2 || h < 2 {
		return nil, errors.New("averaging leaves fewer than 2×2 cells; reduce averaging factor")
	}
	a := make([]float32, w*h)
	nan := float32(math.NaN())
	for y := 0; y < h; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		for x := 0; x < w; x++ {
			sum := 0.0
			ok := true
			for yy := y * f; yy < (y+1)*f; yy++ {
				for xx := x * f; xx < (x+1)*f; xx++ {
					v := z[yy*g.W+xx]
					if !valid(v) {
						ok = false
					}
					sum += float64(v)
				}
			}
			if ok {
				a[y*w+x] = float32(sum / float64(f*f))
			} else {
				a[y*w+x] = nan
			}
		}
	}
	if p.SmoothRadius > 0 {
		out := make([]float32, len(a))
		r := p.SmoothRadius
		for y := 0; y < h; y++ {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			for x := 0; x < w; x++ {
				i := y*w + x
				if !valid(a[i]) {
					out[i] = nan
					continue
				}
				sum := 0.0
				n := 0
				for yy := max(0, y-r); yy <= min(h-1, y+r); yy++ {
					for xx := max(0, x-r); xx <= min(w-1, x+r); xx++ {
						v := a[yy*w+xx]
						if valid(v) {
							sum += float64(v)
							n++
						}
					}
				}
				out[i] = float32(sum / float64(n))
			}
		}
		a = out
	}
	if p.RegionalRadius > 0 {
		for pass := 0; pass < 3; pass++ {
			var e error
			a, e = regionalMean(ctx, a, w, h, p.RegionalRadius)
			if e != nil {
				return nil, e
			}
		}
	}
	tags := map[uint16]tag{}
	for k, v := range g.Tags {
		v.Data = append([]byte(nil), v.Data...)
		tags[k] = v
	}
	// Preserve the upper-left grid origin, including non-zero raster tiepoints.
	if g.Order != nil && f != 1 {
		if t, ok := tags[33550]; ok && len(t.Data) >= 16 {
			for i := 0; i < 2; i++ {
				v := math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
				g.Order.PutUint64(t.Data[i*8:], math.Float64bits(v*float64(f)))
			}
			tags[33550] = t
		}
		if t, ok := tags[33922]; ok {
			for i := 0; i+48 <= len(t.Data); i += 48 {
				for j := 0; j < 2; j++ {
					v := math.Float64frombits(g.Order.Uint64(t.Data[i+j*8:]))
					g.Order.PutUint64(t.Data[i+j*8:], math.Float64bits(v/float64(f)))
				}
			}
			tags[33922] = t
		}
		if t, ok := tags[34264]; ok && len(t.Data) >= 128 {
			for _, i := range []int{0, 1, 4, 5, 8, 9} {
				v := math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
				g.Order.PutUint64(t.Data[i*8:], math.Float64bits(v*float64(f)))
			}
			tags[34264] = t
		}
	}
	return &Grid{W: w, H: h, Z: a, Tags: tags, Order: g.Order, PixelSize: g.PixelSize * float64(f)}, nil
}

func generateContours(ctx context.Context, g *Grid, z []float32, p ContourParams, revision int, progress func(int, string)) (*ContourSet, error) {
	if e := p.validate(); e != nil {
		return nil, e
	}
	if p.MinArea > 0 && areaScaleM2(g) == 0 {
		return nil, errors.New("the square-metre area filter requires a projected raster with recognized metre/foot units; reproject the DTM, or set minimum enclosed area to 0 for unreferenced/angular data")
	}
	progress(5, "Preparing contour surface")
	surface, e := contourSurface(ctx, g, z, p)
	if e != nil {
		return nil, e
	}
	lo, hi := extent(surface.Z)
	if (hi-lo)/p.Interval > 4096 || math.Abs((lo-p.Base)/p.Interval) > 1e12 || math.Abs((hi-p.Base)/p.Interval) > 1e12 {
		return nil, errors.New("contour range exceeds 4096 levels or supported height range; increase interval")
	}
	nodes := []contourNode{}
	lookup := map[contourNodeKey]int{}
	segments := []contourSegment{}
	w, h := surface.W, surface.H
	factor := float64(p.AverageFactor)
	addNode := func(level int64, edge int, pt CellPoint) int {
		k := contourNodeKey{level, edge}
		if i, ok := lookup[k]; ok {
			return i
		}
		i := len(nodes)
		lookup[k] = i
		nodes = append(nodes, contourNode{point: pt})
		return i
	}
	for y := 0; y < h-1; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if y%32 == 0 {
			progress(15+y*65/max(1, h-1), "Tracing full-resolution contours")
		}
		for x := 0; x < w-1; x++ {
			v := [4]float64{float64(surface.Z[y*w+x]), float64(surface.Z[y*w+x+1]), float64(surface.Z[(y+1)*w+x+1]), float64(surface.Z[(y+1)*w+x])}
			ok := true
			for _, n := range v {
				if !validNumber(n) {
					ok = false
				}
			}
			if !ok {
				continue
			}
			vmin, vmax := v[0], v[0]
			for _, n := range v[1:] {
				vmin = math.Min(vmin, n)
				vmax = math.Max(vmax, n)
			}
			for level := int64(math.Ceil((vmin - p.Base) / p.Interval)); level <= int64(math.Floor((vmax-p.Base)/p.Interval)); level++ {
				elevation := p.Base + float64(level)*p.Interval
				edges := []int{}
				for edge := 0; edge < 4; edge++ {
					if (v[edge] >= elevation) != (v[(edge+1)%4] >= elevation) {
						edges = append(edges, edge)
					}
				}
				if len(edges) == 0 {
					continue
				}
				points := [4]CellPoint{{float64(x) + .5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + 1.5}, {float64(x) + .5, float64(y) + 1.5}}
				vertex := [4]int{y*w + x, y*w + x + 1, (y+1)*w + x + 1, (y+1)*w + x}
				edgeKeys := [4]int{y*w + x, w*h + y*w + x + 1, (y+1)*w + x, w*h + y*w + x}
				nodeFor := func(edge int) int {
					next := (edge + 1) % 4
					t := (elevation - v[edge]) / (v[next] - v[edge])
					a, b := points[edge], points[next]
					key := edgeKeys[edge]
					if t == 0 {
						key = 2*w*h + vertex[edge]
					} else if t == 1 {
						key = 2*w*h + vertex[next]
					}
					return addNode(level, key, CellPoint{(a.X + t*(b.X-a.X)) * factor, (a.Y + t*(b.Y-a.Y)) * factor})
				}
				connect := func(a, b int) {
					i, j := nodeFor(a), nodeFor(b)
					if i == j {
						return
					}
					s := len(segments)
					segments = append(segments, contourSegment{i, j})
					nodes[i].segments = append(nodes[i].segments, s)
					nodes[j].segments = append(nodes[j].segments, s)
				}
				if len(edges) == 2 {
					connect(edges[0], edges[1])
				} else if len(edges) == 4 {
					q := (v[0]-elevation)*(v[2]-elevation) - (v[1]-elevation)*(v[3]-elevation)
					if q >= 0 {
						connect(0, 1)
						connect(2, 3)
					} else {
						connect(3, 0)
						connect(1, 2)
					}
				}
				if len(segments) > 200000 {
					return nil, errors.New("more than 200,000 contour segments; increase interval, averaging or smoothing")
				}
			}
		}
	}
	progress(85, "Joining contour lines")
	used := make([]bool, len(segments))
	lines := []ContourLine{}
	// Sorting keys gives reproducible line order across runs.
	keys := make([]contourNodeKey, 0, len(lookup))
	for k := range lookup {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].level == keys[j].level {
			return keys[i].edge < keys[j].edge
		}
		return keys[i].level < keys[j].level
	})
	walk := func(start, seg int, level int64) {
		line := ContourLine{Elevation: p.Base + float64(level)*p.Interval, Index: level%int64(p.IndexEvery) == 0, Points: []CellPoint{nodes[start].point}}
		current := start
		for !used[seg] {
			used[seg] = true
			s := segments[seg]
			next := s.a
			if next == current {
				next = s.b
			}
			line.Points = append(line.Points, nodes[next].point)
			if next == start || len(nodes[next].segments) != 2 {
				break
			}
			a, b := nodes[next].segments[0], nodes[next].segments[1]
			seg = a
			if used[seg] {
				seg = b
			}
			current = next
		}
		if len(line.Points) >= 2 {
			lines = append(lines, line)
		}
	}
	for _, k := range keys {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		i := lookup[k]
		if len(nodes[i].segments) == 2 {
			continue
		}
		for _, seg := range nodes[i].segments {
			if !used[seg] {
				walk(i, seg, k.level)
			}
		}
	}
	for _, k := range keys {
		i := lookup[k]
		for _, seg := range nodes[i].segments {
			if !used[seg] {
				walk(i, seg, k.level)
			}
		}
	}
	kept := make([]ContourLine, 0, len(lines))
	keptSegments := 0
	removedSmallLoops := 0
	for _, line := range lines {
		if area, closed := contourAreaM2(g, line); closed && areaScaleM2(g) > 0 {
			line.AreaM2 = &area
		}
		if p.MinArea > 0 {
			if area, closed := contourAreaM2(g, line); closed && area < p.MinArea-math.Max(1e-8, p.MinArea*1e-9) {
				removedSmallLoops++
				continue
			}
		}
		if p.MinLength > 0 && contourLength(g, line) < p.MinLength {
			continue
		}
		kept = append(kept, line)
		keptSegments += len(line.Points) - 1
	}
	return &ContourSet{Params: p, Revision: revision, Lines: kept, Surface: surface, Segments: keptSegments, RemovedSmallLoops: removedSmallLoops}, nil
}

func areaScaleM2(g *Grid) float64 {
	metres := 0.0
	switch horizontalUnit(g) {
	case "m":
		metres = 1
	case "ft":
		metres = .3048
	case "US ft":
		metres = 1200.0 / 3937
	}
	if metres == 0 {
		return 0
	}
	x, y, ok := worldAt(g, 0, 0)
	if !ok {
		return 0
	}
	xx, xy, _ := worldAt(g, 1, 0)
	yx, yy, _ := worldAt(g, 0, 1)
	return math.Abs((xx-x)*(yy-y)-(yx-x)*(xy-y)) * metres * metres
}
func contourAreaM2(g *Grid, line ContourLine) (float64, bool) {
	p := line.Points
	if len(p) < 4 || p[0] != p[len(p)-1] {
		return 0, false
	}
	origin := p[0]
	twice := 0.0
	for i := 1; i < len(p); i++ {
		a, b := p[i-1], p[i]
		twice += (a.X-origin.X)*(b.Y-origin.Y) - (b.X-origin.X)*(a.Y-origin.Y)
	}
	return math.Abs(twice) * .5 * areaScaleM2(g), true
}

// Separable running sums keep broad smoothing O(width*height), independent of radius.
// Three normalized box passes approximate Gaussian low-pass smoothing. Missing
// cells remain missing in every pass; they are never used as zero elevations.
func regionalMean(ctx context.Context, a []float32, w, h, r int) ([]float32, error) {
	sums := make([]float64, len(a))
	counts := make([]uint16, len(a))
	out := make([]float32, len(a))
	nan := float32(math.NaN())
	for y := 0; y < h; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		sum := 0.0
		n := 0
		next := 0
		for x := 0; x < w; x++ {
			for next < w && next <= x+r {
				v := a[y*w+next]
				if valid(v) {
					sum += float64(v)
					n++
				}
				next++
			}
			if old := x - r - 1; old >= 0 {
				v := a[y*w+old]
				if valid(v) {
					sum -= float64(v)
					n--
				}
			}
			sums[y*w+x] = sum
			counts[y*w+x] = uint16(n)
		}
	}
	for x := 0; x < w; x++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		sum := 0.0
		n := 0
		next := 0
		for y := 0; y < h; y++ {
			for next < h && next <= y+r {
				i := next*w + x
				sum += sums[i]
				n += int(counts[i])
				next++
			}
			if old := y - r - 1; old >= 0 {
				i := old*w + x
				sum -= sums[i]
				n -= int(counts[i])
			}
			i := y*w + x
			if valid(a[i]) && n > 0 {
				out[i] = float32(sum / float64(n))
			} else {
				out[i] = nan
			}
		}
	}
	return out, nil
}
func contourLength(g *Grid, line ContourLine) float64 {
	a, b, c, d := 1.0, 0.0, 0.0, 1.0
	if x, y, ok := worldAt(g, 0, 0); ok {
		xx, xy, _ := worldAt(g, 1, 0)
		yx, yy, _ := worldAt(g, 0, 1)
		a, c, b, d = xx-x, xy-y, yx-x, yy-y
	}
	length := 0.0
	for i := 1; i < len(line.Points); i++ {
		p, q := line.Points[i-1], line.Points[i]
		dx, dy := q.X-p.X, q.Y-p.Y
		length += math.Hypot(a*dx+b*dy, c*dx+d*dy)
	}
	return length
}

func geoKey(g *Grid, id uint16) uint16 {
	if g.Order == nil {
		return 0
	}
	data := g.Tags[34735].Data
	for i := 8; i+8 <= len(data); i += 8 {
		if g.Order.Uint16(data[i:]) == id && g.Order.Uint16(data[i+2:]) == 0 && g.Order.Uint16(data[i+4:]) == 1 {
			return g.Order.Uint16(data[i+6:])
		}
	}
	return 0
}
func horizontalUnit(g *Grid) string {
	if _, _, ok := worldAt(g, 0, 0); !ok {
		return "px"
	}
	if geoKey(g, 1024) == 2 {
		return "degrees"
	}
	switch geoKey(g, 3076) {
	case 9001:
		return "m"
	case 9002:
		return "ft"
	case 9003:
		return "US ft"
	}
	code := geoKey(g, 3072)
	if code >= 32601 && code <= 32660 || code >= 32701 && code <= 32760 {
		return "m"
	}
	return "map units"
}

func contourCSV(g *Grid, c *ContourSet) ([]byte, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"elevation", "index_contour", "wkt", "coordinate_system"})
	for _, line := range c.Lines {
		parts := make([]string, len(line.Points))
		system := "source CRS (see source raster)"
		for i, p := range line.Points {
			x, y, ok := worldAt(g, p.X, p.Y)
			if !ok {
				x, y = p.X, p.Y
				system = "source pixel coordinates (no georeference)"
			}
			parts[i] = fmt.Sprintf("%.9f %.9f", x, y)
		}
		if e := w.Write([]string{fmt.Sprintf("%.8g", line.Elevation), fmt.Sprint(line.Index), "LINESTRING (" + strings.Join(parts, ", ") + ")", system}); e != nil {
			return nil, e
		}
	}
	w.Flush()
	return []byte(b.String()), w.Error()
}
