package main

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const breaklineFlag = 512
const waterbodyFlag = 1024
const maxFeatureCells = 1000000

type TerrainFeature struct {
	ID           int           `json:"id"`
	Kind         string        `json:"kind"`
	Mode         string        `json:"mode,omitempty"`
	Points       []CellPoint   `json:"points"`
	Holes        [][]CellPoint `json:"holes,omitempty"`
	Elevation    float64       `json:"elevation"`
	EndElevation float64       `json:"endElevation"`
	Radius       int           `json:"radius"`
	Cells        []int         `json:"-"`
}
type TerrainFeatureRequest struct {
	Kind         string        `json:"kind"`
	Mode         string        `json:"mode"`
	Points       []CellPoint   `json:"points"`
	Holes        [][]CellPoint `json:"holes"`
	Elevation    float64       `json:"elevation"`
	EndElevation float64       `json:"endElevation"`
	Radius       int           `json:"radius"`
	FillNoData   bool          `json:"fillNoData"`
	Revision     int           `json:"revision"`
}

func pointOnSegment(p, a, b CellPoint) bool {
	dx, dy := b.X-a.X, b.Y-a.Y
	cross := dx*(p.Y-a.Y) - dy*(p.X-a.X)
	return math.Abs(cross) <= 1e-9*math.Max(1, math.Hypot(dx, dy)) && p.X >= math.Min(a.X, b.X)-1e-9 && p.X <= math.Max(a.X, b.X)+1e-9 && p.Y >= math.Min(a.Y, b.Y)-1e-9 && p.Y <= math.Max(a.Y, b.Y)+1e-9
}
func segmentsTouch(a, b, c, d CellPoint) bool {
	return segmentCross(a, b, c, d) || pointOnSegment(a, c, d) || pointOnSegment(b, c, d) || pointOnSegment(c, a, b) || pointOnSegment(d, a, b)
}
func ringBoundary(p CellPoint, ring []CellPoint) bool {
	for i, a := range ring {
		if pointOnSegment(p, a, ring[(i+1)%len(ring)]) {
			return true
		}
	}
	return false
}
func inOpenWater(p CellPoint, f TerrainFeature) bool {
	if !insidePolygon(p.X, p.Y, f.Points) || ringBoundary(p, f.Points) {
		return false
	}
	for _, hole := range f.Holes {
		if insidePolygon(p.X, p.Y, hole) || ringBoundary(p, hole) {
			return false
		}
	}
	return true
}
func validateFeaturePoints(g *Grid, points []CellPoint, minPoints int) error {
	if len(points) < minPoints || len(points) > 256 {
		return fmt.Errorf("select %d–256 vertices", minPoints)
	}
	for i, p := range points {
		if !validNumber(p.X) || !validNumber(p.Y) || p.X < 0 || p.Y < 0 || p.X > float64(g.W) || p.Y > float64(g.H) {
			return errors.New("constraint vertices must lie inside the raster")
		}
		if i > 0 && math.Hypot(p.X-points[i-1].X, p.Y-points[i-1].Y) < 1e-7 {
			return errors.New("remove duplicate adjacent constraint vertices")
		}
	}
	return nil
}
func validateFeatureRing(g *Grid, points []CellPoint) error {
	if e := validateFeaturePoints(g, points, 3); e != nil {
		return e
	}
	area := 0.0
	for i, a := range points {
		b := points[(i+1)%len(points)]
		area += a.X*b.Y - b.X*a.Y
		if math.Hypot(a.X-b.X, a.Y-b.Y) < 1e-7 {
			return errors.New("rings are closed automatically; do not repeat the first vertex")
		}
		for j := i + 1; j < len(points); j++ {
			if j == i+1 || (i == 0 && j == len(points)-1) {
				continue
			}
			if segmentsTouch(a, b, points[j], points[(j+1)%len(points)]) {
				return errors.New("constraint polygon touches or crosses itself")
			}
		}
	}
	if math.Abs(area) < 1 {
		return errors.New("constraint polygon is too small")
	}
	return nil
}
func ringsTouch(a, b []CellPoint) bool {
	for i, p := range a {
		for j, q := range b {
			if segmentsTouch(p, a[(i+1)%len(a)], q, b[(j+1)%len(b)]) {
				return true
			}
		}
	}
	return false
}
func validateWaterRings(g *Grid, outer []CellPoint, holes [][]CellPoint) error {
	if e := validateFeatureRing(g, outer); e != nil {
		return e
	}
	if len(holes) > 16 {
		return errors.New("a waterbody supports up to 16 island rings")
	}
	for i, hole := range holes {
		if e := validateFeatureRing(g, hole); e != nil {
			return fmt.Errorf("island %d: %w", i+1, e)
		}
		if ringsTouch(outer, hole) || !insidePolygon(hole[0].X, hole[0].Y, outer) {
			return errors.New("island rings must be strictly inside the waterbody")
		}
		for _, other := range holes[:i] {
			if ringsTouch(hole, other) || insidePolygon(hole[0].X, hole[0].Y, other) || insidePolygon(other[0].X, other[0].Y, hole) {
				return errors.New("island rings must not overlap or nest")
			}
		}
	}
	return nil
}

// All feature changes use the normal immutable terrain snapshots and history.
// Breaklines are raster barriers with a stated radius, not zero-width TIN edges.
func (s *studio) applyTerrainFeature(q TerrainFeatureRequest) error {
	if s.busy || s.grid == nil || s.result == nil || q.Revision != s.edits.Revision {
		return errors.New("open a DTM and refresh before adding a terrain constraint")
	}
	if len(s.terrainFeatures) >= 128 {
		return errors.New("remove an unused constraint before adding more than 128 features")
	}
	if !validNumber(q.Elevation) || !validNumber(q.EndElevation) || math.Abs(q.Elevation) > 1e8 || math.Abs(q.EndElevation) > 1e8 {
		return errors.New("constraint heights must be finite and within 100 million height units")
	}
	f := TerrainFeature{ID: s.nextFeatureID + 1, Kind: q.Kind, Mode: q.Mode, Points: append([]CellPoint{}, q.Points...), Elevation: q.Elevation, EndElevation: q.EndElevation, Radius: q.Radius}
	for _, ring := range q.Holes {
		f.Holes = append(f.Holes, append([]CellPoint{}, ring...))
	}
	var indices []int
	var e error
	switch q.Kind {
	case "breakline":
		if e = validateFeaturePoints(s.grid, q.Points, 2); e != nil {
			return e
		}
		for i := 1; i < len(q.Points); i++ {
			for j := i + 2; j < len(q.Points); j++ {
				if segmentsTouch(q.Points[i-1], q.Points[i], q.Points[j-1], q.Points[j]) {
					return errors.New("breakline crosses or touches itself; split it into separate features")
				}
			}
			if i+1 < len(q.Points) && (pointOnSegment(q.Points[i+1], q.Points[i-1], q.Points[i]) || pointOnSegment(q.Points[i-1], q.Points[i], q.Points[i+1])) {
				return errors.New("breakline doubles back; redraw its vertices")
			}
		}
		if len(q.Holes) > 0 || q.Radius < 1 || q.Radius > 5 || (q.Mode != "preserve" && q.Mode != "grade" && q.Mode != "stream") {
			return errors.New("breaklines need preserve/grade/stream mode and a radius of 1–5 source pixels")
		}
		if q.Mode == "stream" && q.EndElevation > q.Elevation {
			return errors.New("draw a stream downstream: its end height must not exceed its start height")
		}
		indices, _, e = selectBrushCells(s.grid, q.Points, q.Radius)
	case "waterbody":
		if e = validateWaterRings(s.grid, q.Points, q.Holes); e != nil {
			return e
		}
		indices, e = selectWaterCells(s.grid, f)
		f.Mode = "level"
		f.EndElevation = f.Elevation
		f.Radius = 0
	default:
		return errors.New("constraint kind must be breakline or waterbody")
	}
	if e != nil {
		return e
	}
	total := len(indices)
	for _, existing := range s.terrainFeatures {
		total += len(existing.Cells)
	}
	if total > maxFeatureCells {
		return errors.New("active features exceed one million protected cell references; remove or split constraints")
	}
	// Arc-length interpolation gives a continuous, directed endpoint grade.
	lengths := make([]float64, len(q.Points))
	for i := 1; i < len(q.Points); i++ {
		lengths[i] = lengths[i-1] + featureSegmentLength(s.grid, q.Points[i-1], q.Points[i])
	}
	rec := EditRecord{Tool: q.Kind + "-constraint", Points: append([]CellPoint{}, q.Points...), Time: time.Now().UTC().Format(time.RFC3339)}
	for _, i := range indices {
		old, flags := s.result.DTM[i], int(s.result.Flags[i])
		value := old
		if !valid(old) && (!q.FillNoData || q.Kind == "breakline" && q.Mode == "preserve") {
			if q.Kind == "breakline" {
				return errors.New("breakline corridor crosses NoData; redraw or use a graded profile with filling enabled")
			}
			continue
		}
		if q.Kind == "waterbody" {
			value = float32(q.Elevation)
		} else if q.Mode != "preserve" {
			p := CellPoint{float64(i%s.grid.W) + .5, float64(i/s.grid.W) + .5}
			best, along := math.Inf(1), 0.0
			for k := 1; k < len(q.Points); k++ {
				a, b := q.Points[k-1], q.Points[k]
				dx, dy := b.X-a.X, b.Y-a.Y
				t := math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/(dx*dx+dy*dy)))
				d := math.Hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
				if d < best {
					best = d
					along = lengths[k-1] + t*(lengths[k]-lengths[k-1])
				}
			}
			value = float32(q.Elevation + (q.EndElevation-q.Elevation)*along/lengths[len(lengths)-1])
		}
		if !valid(value) {
			return errors.New("constraint height exceeds the terrain range")
		}
		if value != old && flags&(256|breaklineFlag|waterbodyFlag) != 0 {
			return errors.New("constraint conflicts with a contour pin or protected terrain feature; remove the conflicting constraint first")
		}
		next := flags | 64
		if q.Kind == "breakline" {
			next |= breaklineFlag
		} else {
			next |= waterbodyFlag | 128
		}
		if !valid(s.grid.Z[i]) {
			next |= 32
		}
		next &^= 8
		if valid(s.result.Clean[i]) && s.result.Clean[i]-value > float32(s.params.GroundThreshold) {
			next |= 8
		}
		f.Cells = append(f.Cells, i)
		if value != old || next != flags {
			rec.Changes = append(rec.Changes, cellChange{i, old, value, s.result.Flags[i], float32(next)})
		}
	}
	if len(f.Cells) == 0 {
		return errors.New("constraint contains no usable elevation cells")
	}
	before := append([]TerrainFeature{}, s.terrainFeatures...)
	after := append(append([]TerrainFeature{}, before...), f)
	rec.FeaturesBefore, rec.FeaturesAfter = &before, &after
	rec.Cells = len(rec.Changes)
	s.nextFeatureID = f.ID
	s.recordEdit(rec)
	// Geometry-only changes (e.g. overlapping same-height water polygons) must
	// still invalidate clipping, even if the raster values and flags were equal.
	s.message = fmt.Sprintf("%s #%d constrained: %d source cells", q.Kind, f.ID, len(f.Cells))
	return nil
}
func (s *studio) removeTerrainFeature(id, revision int) error {
	if s.busy || s.result == nil || revision != s.edits.Revision {
		return errors.New("terrain changed; refresh before removing a constraint")
	}
	before := append([]TerrainFeature{}, s.terrainFeatures...)
	after := []TerrainFeature{}
	var removed *TerrainFeature
	for _, f := range before {
		if f.ID == id {
			v := f
			removed = &v
		} else {
			after = append(after, f)
		}
	}
	if removed == nil {
		return errors.New("terrain constraint does not exist")
	}
	owned := map[int]int{}
	for _, f := range after {
		bit := breaklineFlag
		if f.Kind == "waterbody" {
			bit = waterbodyFlag | 128
		}
		for _, i := range f.Cells {
			owned[i] |= bit
		}
	}
	rec := EditRecord{Tool: "remove-" + removed.Kind, Time: time.Now().UTC().Format(time.RFC3339), FeaturesBefore: &before, FeaturesAfter: &after}
	for _, i := range removed.Cells {
		bits := breaklineFlag
		if removed.Kind == "waterbody" {
			bits = waterbodyFlag | 128
		}
		flags := int(s.result.Flags[i])
		next := (flags &^ bits) | (owned[i] & bits)
		if next != flags {
			z := s.result.DTM[i]
			rec.Changes = append(rec.Changes, cellChange{i, z, z, float32(flags), float32(next)})
		}
	}
	rec.Cells = len(rec.Changes)
	s.recordEdit(rec)
	s.message = "Terrain constraint removed; edited heights retained"
	return nil
}

type TerrainFeatureSummary struct {
	ID           int     `json:"id"`
	Kind         string  `json:"kind"`
	Mode         string  `json:"mode"`
	Cells        int     `json:"cells"`
	Elevation    float64 `json:"elevation"`
	EndElevation float64 `json:"endElevation"`
}

func featureSummaries(features []TerrainFeature) []TerrainFeatureSummary {
	summaries := make([]TerrainFeatureSummary, 0, len(features))
	for _, f := range features {
		summaries = append(summaries, TerrainFeatureSummary{f.ID, f.Kind, f.Mode, len(f.Cells), f.Elevation, f.EndElevation})
	}
	return summaries
}

func featureSegmentLength(g *Grid, a, b CellPoint) float64 {
	ax, ay, ok := worldAt(g, a.X, a.Y)
	bx, by, bok := worldAt(g, b.X, b.Y)
	if ok && bok {
		return math.Hypot(bx-ax, by-ay)
	}
	return math.Hypot(b.X-a.X, b.Y-a.Y)
}
