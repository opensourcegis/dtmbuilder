package main

import (
	"context"
	"fmt"
	"math"
	"sort"
)

type waterEdge struct{ a, b CellPoint }
type waterClip struct {
	feature TerrainFeature
	bounds  [4]float64
	rows    map[int][]waterEdge
	tiles   map[[2]int][]waterEdge
}

func prepareWaterClip(f TerrainFeature) waterClip {
	c := waterClip{feature: f, bounds: [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}, rows: map[int][]waterEdge{}, tiles: map[[2]int][]waterEdge{}}
	rings := append([][]CellPoint{f.Points}, f.Holes...)
	for _, ring := range rings {
		for i, a := range ring {
			b := ring[(i+1)%len(ring)]
			edge := waterEdge{a, b}
			c.bounds[0] = math.Min(c.bounds[0], a.X)
			c.bounds[1] = math.Min(c.bounds[1], a.Y)
			c.bounds[2] = math.Max(c.bounds[2], a.X)
			c.bounds[3] = math.Max(c.bounds[3], a.Y)
			x0, x1 := int(math.Floor(math.Min(a.X, b.X)/128)), int(math.Floor(math.Max(a.X, b.X)/128))
			y0, y1 := int(math.Floor(math.Min(a.Y, b.Y)/128)), int(math.Floor(math.Max(a.Y, b.Y)/128))
			for y := y0; y <= y1; y++ {
				c.rows[y] = append(c.rows[y], edge)
				for x := x0; x <= x1; x++ {
					c.tiles[[2]int{x, y}] = append(c.tiles[[2]int{x, y}], edge)
				}
			}
		}
	}
	return c
}
func (c waterClip) contains(p CellPoint) bool {
	if p.X < c.bounds[0] || p.X > c.bounds[2] || p.Y < c.bounds[1] || p.Y > c.bounds[3] {
		return false
	}
	inside := false
	// Even/odd applies to the outer ring and disjoint island rings together.
	for _, e := range c.rows[int(math.Floor(p.Y/128))] {
		if pointOnSegment(p, e.a, e.b) {
			return false
		}
		if (e.a.Y > p.Y) != (e.b.Y > p.Y) && p.X < (e.b.X-e.a.X)*(p.Y-e.a.Y)/(e.b.Y-e.a.Y)+e.a.X {
			inside = !inside
		}
	}
	return inside
}
func segmentParameter(a, b, c, d CellPoint) (float64, bool) {
	dx, dy, ex, ey := b.X-a.X, b.Y-a.Y, d.X-c.X, d.Y-c.Y
	det := dx*ey - dy*ex
	if math.Abs(det) < 1e-14 {
		return 0, false
	}
	t := ((c.X-a.X)*ey - (c.Y-a.Y)*ex) / det
	u := ((c.X-a.X)*dy - (c.Y-a.Y)*dx) / det
	return math.Max(0, math.Min(1, t)), t >= -1e-10 && t <= 1+1e-10 && u >= -1e-10 && u <= 1+1e-10
}
func nearPoint(a, b CellPoint) bool { return math.Hypot(a.X-b.X, a.Y-b.Y) < 1e-8 }

// Clip exact vector interiors, including water at an exact contour level.
// Retain boundary-coincident pieces and island contours; never change water Z.
// The cached marching-square segments remain unmodified for the next local edit.
func clipWaterContours(ctx context.Context, g *Grid, c *ContourSet, features []TerrainFeature) error {
	water := []waterClip{}
	for _, f := range features {
		if f.Kind == "waterbody" {
			water = append(water, prepareWaterClip(f))
		}
	}
	if len(water) == 0 {
		return nil
	}
	result := []ContourLine{}
	checked := 0
	for _, line := range c.Lines {
		minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range line.Points {
			minx = math.Min(minx, p.X)
			miny = math.Min(miny, p.Y)
			maxx = math.Max(maxx, p.X)
			maxy = math.Max(maxy, p.Y)
		}
		candidates := []waterClip{}
		for _, w := range water {
			if maxx >= w.bounds[0] && minx <= w.bounds[2] && maxy >= w.bounds[1] && miny <= w.bounds[3] {
				candidates = append(candidates, w)
			}
		}
		if len(candidates) == 0 {
			result = append(result, line)
			continue
		}
		pieces := [][]CellPoint{}
		var current []CellPoint
		flush := func() {
			if len(current) >= 2 {
				pieces = append(pieces, current)
			}
			current = nil
		}
		for i := 1; i < len(line.Points); i++ {
			checked++
			if checked%4096 == 0 {
				if e := ctx.Err(); e != nil {
					return e
				}
			}
			a, b := line.Points[i-1], line.Points[i]
			parameters := []float64{0, 1}
			x0, x1 := int(math.Floor(math.Min(a.X, b.X)/128)), int(math.Floor(math.Max(a.X, b.X)/128))
			y0, y1 := int(math.Floor(math.Min(a.Y, b.Y)/128)), int(math.Floor(math.Max(a.Y, b.Y)/128))
			for _, w := range candidates {
				if math.Max(a.X, b.X) < w.bounds[0] || math.Min(a.X, b.X) > w.bounds[2] || math.Max(a.Y, b.Y) < w.bounds[1] || math.Min(a.Y, b.Y) > w.bounds[3] {
					continue
				}
				for y := y0; y <= y1; y++ {
					for x := x0; x <= x1; x++ {
						for _, edge := range w.tiles[[2]int{x, y}] {
							if t, ok := segmentParameter(a, b, edge.a, edge.b); ok {
								parameters = append(parameters, t)
							}
						}
					}
				}
			}
			sort.Float64s(parameters)
			clipped := false
			for k := 1; k < len(parameters); k++ {
				t0, t1 := parameters[k-1], parameters[k]
				if t1-t0 < 1e-10 {
					continue
				}
				point := func(t float64) CellPoint { return CellPoint{a.X + t*(b.X-a.X), a.Y + t*(b.Y-a.Y)} }
				mid := point((t0 + t1) / 2)
				inside := false
				for _, w := range candidates {
					if w.contains(mid) {
						inside = true
						break
					}
				}
				if inside {
					flush()
					clipped = true
					continue
				}
				start, end := point(t0), point(t1)
				if len(current) == 0 {
					current = []CellPoint{start}
				} else if !nearPoint(current[len(current)-1], start) {
					flush()
					current = []CellPoint{start}
				}
				if !nearPoint(current[len(current)-1], end) {
					current = append(current, end)
				}
			}
			if clipped {
				c.WaterClippedSegments++
			}
		}
		flush()
		if len(pieces) > 1 && nearPoint(line.Points[0], line.Points[len(line.Points)-1]) && nearPoint(pieces[len(pieces)-1][len(pieces[len(pieces)-1])-1], pieces[0][0]) {
			last := pieces[len(pieces)-1]
			pieces[0] = append(last, pieces[0][1:]...)
			pieces = pieces[:len(pieces)-1]
		}
		for _, points := range pieces {
			piece := line
			piece.Points = points
			piece.AreaM2 = nil
			if area, closed := contourAreaM2(g, piece); closed && areaScaleM2(g) > 0 {
				piece.AreaM2 = &area
			}
			result = append(result, piece)
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	c.Lines = result
	c.Segments = 0
	for i := range c.Lines {
		c.Lines[i].ID = i
		c.Segments += len(c.Lines[i].Points) - 1
	}
	return nil
}

func assessWaterBanks(ctx context.Context, g *Grid, z, flags []float32, c *ContourSet, features []TerrainFeature) error {
	below := map[int]bool{}
	visited := 0
	for _, f := range features {
		if f.Kind != "waterbody" {
			continue
		}
		level := float64(float32(f.Elevation)) - .1*c.Params.Interval
		for _, i := range f.Cells {
			visited++
			if visited%4096 == 0 {
				if e := ctx.Err(); e != nil {
					return e
				}
			}
			x, y := i%g.W, i/g.W
			for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				xx, yy := x+d[0], y+d[1]
				if xx < 0 || yy < 0 || xx >= g.W || yy >= g.H {
					continue
				}
				j := yy*g.W + xx
				if valid(z[j]) && int(flags[j])&1024 == 0 && float64(z[j]) < level && !inOpenWater(CellPoint{float64(xx) + .5, float64(yy) + .5}, f) {
					below[j] = true
				}
			}
		}
	}
	c.Quality.ShorelineBelowLevelCells = len(below)
	if len(below) > 0 {
		c.Quality.Warnings = append(c.Quality.Warnings, fmt.Sprintf("%d cells immediately outside protected water are more than 0.1 contour interval below the known water level. Check the boundary, banks and level; clipping does not repair inconsistent banks.", len(below)))
	}
	return ctx.Err()
}
