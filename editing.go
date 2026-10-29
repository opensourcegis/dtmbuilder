package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const maxEditCells = 250000

type CellPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type EditRequest struct {
	Tool       string      `json:"tool"`
	Points     []CellPoint `json:"points"`
	Radius     int         `json:"radius"`
	Threshold  float64     `json:"threshold"`
	Elevation  float64     `json:"elevation"`
	FillNoData bool        `json:"fillNoData"`
	Revision   int         `json:"revision"`
}
type cellChange struct {
	Index                                int
	Before, After, FlagBefore, FlagAfter float32
}
type EditRecord struct {
	Tool      string       `json:"tool"`
	Cells     int          `json:"cells"`
	Elevation *float64     `json:"elevation,omitempty"`
	Time      string       `json:"time"`
	Changes   []cellChange `json:"-"`
	Points    []CellPoint  `json:"points,omitempty"`
	Radius    int          `json:"radius,omitempty"`
	Threshold float64      `json:"threshold,omitempty"`
}
type editHistory struct {
	Undo, Redo    []EditRecord
	Audit         []EditRecord
	Bytes         int
	Revision      int
	SavedRevision int
}

func (s *studio) resetEdits() {
	s.edits = editHistory{Revision: s.edits.Revision + 1}
	s.contours = nil
}

func initializeDTM(g *Grid) *Result {
	n := len(g.Z)
	r := &Result{Clean: append([]float32(nil), g.Z...), DTM: append([]float32(nil), g.Z...), NDSM: make([]float32, n), Flags: make([]float32, n), Counts: map[string]int{}}
	for i, v := range g.Z {
		if !valid(v) {
			r.NDSM[i] = float32(math.NaN())
			r.Flags[i] = 16
		}
	}
	recount(r)
	return r
}
func recount(r *Result) {
	for _, key := range []string{"nonground", "nodata", "filledOutput", "remainingNoData", "manual", "hydro"} {
		r.Counts[key] = 0
	}
	for i, f := range r.Flags {
		bits := int(f)
		if bits&8 != 0 {
			r.Counts["nonground"]++
		}
		if bits&16 != 0 {
			r.Counts["nodata"]++
		}
		if bits&32 != 0 {
			r.Counts["filledOutput"]++
		}
		if bits&64 != 0 {
			r.Counts["manual"]++
		}
		if bits&128 != 0 {
			r.Counts["hydro"]++
		}
		if !valid(r.DTM[i]) {
			r.Counts["remainingNoData"]++
		}
	}
}
func insidePolygon(x, y float64, p []CellPoint) bool {
	inside := false
	j := len(p) - 1
	for i := range p {
		a, b := p[i], p[j]
		if (a.Y > y) != (b.Y > y) && x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
		j = i
	}
	return inside
}
func segmentCross(a, b, c, d CellPoint) bool {
	cross := func(p, q, r CellPoint) float64 { return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X) }
	v1, v2, v3, v4 := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	return v1*v2 < 0 && v3*v4 < 0
}
func selectCells(g *Grid, q EditRequest) ([]int, map[int]bool, error) {
	if len(q.Points) == 0 || len(q.Points) > 256 {
		return nil, nil, errors.New("select 3–256 polygon vertices, or 1–256 brush stroke points")
	}
	for _, p := range q.Points {
		if !validNumber(p.X) || !validNumber(p.Y) || p.X < 0 || p.Y < 0 || p.X > float64(g.W) || p.Y > float64(g.H) {
			return nil, nil, errors.New("selection lies outside the raster")
		}
	}
	if q.Tool == "spike" {
		if q.Radius < 1 || q.Radius > 50 {
			return nil, nil, errors.New("spike repair needs a brush radius of 1–50 pixels")
		}
		return selectBrushCells(g, q.Points, q.Radius)
	}
	minx, miny, maxx, maxy := float64(g.W), float64(g.H), 0.0, 0.0
	if len(q.Points) < 3 {
		return nil, nil, errors.New("draw a polygon with at least three vertices")
	}
	area := 0.0
	for i, a := range q.Points {
		b := q.Points[(i+1)%len(q.Points)]
		area += a.X*b.Y - b.X*a.Y
		for j := i + 2; j < len(q.Points); j++ {
			if i == 0 && j == len(q.Points)-1 {
				continue
			}
			if segmentCross(a, b, q.Points[j], q.Points[(j+1)%len(q.Points)]) {
				return nil, nil, errors.New("polygon edges cross; redraw the selection")
			}
		}
	}
	if math.Abs(area) < 1 {
		return nil, nil, errors.New("polygon is too small")
	}
	for _, p := range q.Points {
		minx = math.Min(minx, p.X)
		maxx = math.Max(maxx, p.X)
		miny = math.Min(miny, p.Y)
		maxy = math.Max(maxy, p.Y)
	}
	x0, y0 := max(0, int(math.Floor(minx))), max(0, int(math.Floor(miny)))
	x1, y1 := min(g.W-1, int(math.Ceil(maxx))), min(g.H-1, int(math.Ceil(maxy)))
	if (x1-x0+1)*(y1-y0+1) > 2000000 {
		return nil, nil, errors.New("selection bounding box exceeds 2 million cells; use smaller polygons")
	}
	indices := []int{}
	selected := map[int]bool{}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			hit := insidePolygon(float64(x)+.5, float64(y)+.5, q.Points)
			if hit {
				i := y*g.W + x
				indices = append(indices, i)
				selected[i] = true
				if len(indices) > maxEditCells {
					return nil, nil, errors.New("selection exceeds 250,000 source cells; use a smaller area")
				}
			}
		}
	}
	if len(indices) == 0 {
		return nil, nil, errors.New("selection contains no source cell centres")
	}
	return indices, selected, nil
}
func (s *studio) applyEdit(q EditRequest) error {
	if s.busy || s.grid == nil || s.result == nil {
		return errors.New("open a DTM or run extraction before editing")
	}
	if q.Revision != s.edits.Revision {
		return errors.New("terrain changed; refresh the view before applying another edit")
	}
	switch q.Tool {
	case "spike", "flatten", "smooth", "interpolate":
	default:
		return errors.New("unknown edit tool")
	}
	if !validNumber(q.Threshold) || q.Threshold < 0 || !validNumber(q.Elevation) || math.Abs(q.Elevation) > 1e8 {
		return errors.New("height/threshold must be finite; height magnitude must not exceed 100 million")
	}
	g, r := s.grid, s.result
	indices, selected, e := selectCells(g, q)
	if e != nil {
		return e
	}
	source := r.DTM
	// Boundary-only IDW supports arbitrary polygons and never samples inside the edit.
	boundary := []int{}
	if q.Tool == "interpolate" {
		seen := map[int]bool{}
		for _, i := range indices {
			x, y := i%g.W, i/g.W
			for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				xx, yy := x+d[0], y+d[1]
				if xx >= 0 && xx < g.W && yy >= 0 && yy < g.H {
					j := yy*g.W + xx
					if !selected[j] && valid(source[j]) && !seen[j] {
						boundary = append(boundary, j)
						seen[j] = true
					}
				}
			}
		}
		if len(boundary) < 3 {
			return errors.New("interpolation needs at least three valid cells on the polygon's outside boundary")
		}
		sort.Ints(boundary)
		if len(boundary) > 128 {
			sub := make([]int, 128)
			for k := range sub {
				sub[k] = boundary[k*(len(boundary)-1)/127]
			}
			boundary = sub
		}
	}
	changes := make([]cellChange, 0, min(len(indices), maxEditCells))
	for _, i := range indices {
		old := source[i]
		if !valid(old) && (!q.FillNoData || q.Tool == "spike" || q.Tool == "smooth") {
			continue
		}
		x, y := i%g.W, i/g.W
		value := old
		switch q.Tool {
		case "flatten":
			value = float32(q.Elevation)
		case "smooth", "spike":
			v := []float64{}
			rad := 1
			if q.Tool == "spike" {
				rad = 2
			}
			for yy := max(0, y-rad); yy <= min(g.H-1, y+rad); yy++ {
				for xx := max(0, x-rad); xx <= min(g.W-1, x+rad); xx++ {
					j := yy*g.W + xx
					if j != i && valid(source[j]) {
						v = append(v, float64(source[j]))
					}
				}
			}
			if len(v) < 3 {
				continue
			}
			if q.Tool == "spike" {
				m := median(v)
				if math.Abs(float64(old)-m) <= q.Threshold {
					continue
				}
				value = float32(m)
			} else {
				sum := 0.0
				for _, n := range v {
					sum += n
				}
				value = float32(sum / float64(len(v)))
			}
		case "interpolate":
			sum, weight := 0.0, 0.0
			for _, j := range boundary {
				dx, dy := float64(x-j%g.W), float64(y-j/g.W)
				w := 1 / (dx*dx + dy*dy)
				sum += float64(source[j]) * w
				weight += w
			}
			value = float32(sum / weight)
		}
		// A flat cell already at the requested water level still needs a water
		// constraint, so contour filtering cannot subsequently raise/lower it.
		newWaterConstraint := q.Tool == "flatten" && int(r.Flags[i])&128 == 0
		if !valid(value) || valid(old) && value == old && !newWaterConstraint {
			continue
		}
		bits := int(r.Flags[i]) | 64
		// A later elevation-changing edit replaces the previous water constraint.
		bits &^= 128
		if q.Tool == "flatten" {
			bits |= 128
		}
		if !valid(g.Z[i]) {
			bits |= 32
		}
		bits &^= 8
		if valid(r.Clean[i]) && r.Clean[i]-value > float32(s.params.GroundThreshold) {
			bits |= 8
		}
		changes = append(changes, cellChange{i, old, value, r.Flags[i], float32(bits)})
	}
	if len(changes) == 0 {
		if q.Tool == "spike" {
			s.failure = ""
			s.message = "No spikes exceeded the threshold in the brush area"
			return nil
		}
		return errors.New("no cells changed; check the selection, spike threshold or NoData option")
	}
	rec := EditRecord{Tool: q.Tool, Cells: len(changes), Time: time.Now().UTC().Format(time.RFC3339), Changes: changes, Points: append([]CellPoint(nil), q.Points...), Radius: q.Radius, Threshold: q.Threshold}
	if q.Tool == "flatten" {
		level := q.Elevation
		rec.Elevation = &level
	}
	s.commitChanges(rec, true)
	s.edits.Undo = append(s.edits.Undo, rec)
	s.edits.Redo = nil
	s.edits.Bytes += len(changes) * 24
	for (s.edits.Bytes > 32<<20 || len(s.edits.Undo) > 50) && len(s.edits.Undo) > 1 {
		s.edits.Bytes -= len(s.edits.Undo[0].Changes) * 24
		s.edits.Undo = s.edits.Undo[1:]
	}
	audit := rec
	audit.Changes = nil
	s.edits.Audit = append(s.edits.Audit, audit)
	s.message = fmt.Sprintf("%s: %d cells edited", q.Tool, len(changes))
	s.failure = ""
	return nil
}
func (s *studio) commitChanges(rec EditRecord, forward bool) {
	// Copy-on-write keeps concurrent preview/export snapshots consistent.
	prev := s.result
	r := *prev
	r.DTM = append([]float32(nil), prev.DTM...)
	r.NDSM = append([]float32(nil), prev.NDSM...)
	r.Flags = append([]float32(nil), prev.Flags...)
	r.Counts = map[string]int{}
	for k, v := range prev.Counts {
		r.Counts[k] = v
	}
	for _, c := range rec.Changes {
		v, f := c.After, c.FlagAfter
		if !forward {
			v, f = c.Before, c.FlagBefore
		}
		r.DTM[c.Index] = v
		r.Flags[c.Index] = f
		if valid(v) && valid(r.Clean[c.Index]) {
			r.NDSM[c.Index] = max(0, r.Clean[c.Index]-v)
		} else {
			r.NDSM[c.Index] = float32(math.NaN())
		}
	}
	recount(&r)
	s.result = &r
	s.edits.Revision++
	s.contours = nil
}
func (s *studio) historyEdit(redo bool) error {
	if s.busy || s.result == nil {
		return errors.New("editing unavailable while processing")
	}
	from, to := &s.edits.Undo, &s.edits.Redo
	if redo {
		from, to = &s.edits.Redo, &s.edits.Undo
	}
	if len(*from) == 0 {
		return errors.New("no history available")
	}
	rec := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	s.commitChanges(rec, redo)
	*to = append(*to, rec)
	action := "undo"
	if redo {
		action = "redo"
	}
	s.edits.Audit = append(s.edits.Audit, EditRecord{Tool: action + ":" + rec.Tool, Cells: rec.Cells, Time: time.Now().UTC().Format(time.RFC3339)})
	s.message = action + ": " + rec.Tool
	return nil
}
func sample(v float32) any {
	if valid(v) {
		return v
	}
	return nil
}
func (s *studio) inspect(x, y int) (map[string]any, error) {
	g := s.grid
	if g == nil || x < 0 || y < 0 || x >= g.W || y >= g.H {
		return nil, errors.New("cursor outside raster")
	}
	i := y*g.W + x
	out := map[string]any{"column": x, "row": y, "dsm": sample(g.Z[i])}
	if s.result != nil {
		out["dtm"] = sample(s.result.DTM[i])
		out["cleaned"] = sample(s.result.Clean[i])
		out["ndsm"] = sample(s.result.NDSM[i])
		out["flags"] = s.result.Flags[i]
	}
	if wx, wy, ok := worldAt(g, float64(x)+.5, float64(y)+.5); ok {
		out["x"] = wx
		out["y"] = wy
	}
	return out, nil
}
func worldAt(g *Grid, x, y float64) (float64, float64, bool) {
	if g.Order == nil {
		return 0, 0, false
	}
	value := func(id uint16, n int) []float64 {
		t := g.Tags[id]
		if t.Type != 12 || len(t.Data) < n*8 {
			return nil
		}
		v := make([]float64, n)
		for i := range v {
			v[i] = math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
		}
		return v
	}
	if m := value(34264, 16); m != nil {
		return m[0]*x + m[1]*y + m[3], m[4]*x + m[5]*y + m[7], true
	}
	scale, tie := value(33550, 2), value(33922, 6)
	if scale != nil && tie != nil {
		return tie[3] + (x-tie[0])*scale[0], tie[4] - (y-tie[1])*scale[1], true
	}
	return 0, 0, false
}
