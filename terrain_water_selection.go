package main

import (
	"errors"
	"math"
	"sort"
)

// Rasterize valid polygon rings with even/odd scanline spans. Work scales with
// row-edge intersections and selected cells, rather than cells * all vertices.
// Exact boundary centres are excluded, matching inOpenWater and vector clipping.
func selectWaterCells(g *Grid, f TerrainFeature) ([]int, error) {
	minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range f.Points {
		minx = math.Min(minx, p.X)
		maxx = math.Max(maxx, p.X)
		miny = math.Min(miny, p.Y)
		maxy = math.Max(maxy, p.Y)
	}
	x0, x1 := max(0, int(math.Floor(minx))), min(g.W-1, int(math.Ceil(maxx)))
	y0, y1 := max(0, int(math.Floor(miny))), min(g.H-1, int(math.Ceil(maxy)))
	if (x1-x0+1)*(y1-y0+1) > 2000000 {
		return nil, errors.New("selection bounding box exceeds 2 million cells; use smaller polygons")
	}
	rings := append([][]CellPoint{f.Points}, f.Holes...)
	indices := []int{}
	for y := y0; y <= y1; y++ {
		cy := float64(y) + .5
		crossings := []float64{}
		horizontal := [][2]float64{}
		for _, ring := range rings {
			for i, a := range ring {
				b := ring[(i+1)%len(ring)]
				// A hole's uppermost vertex may not contribute a ray crossing,
				// but a sample centred on that vertex is still on its boundary.
				if math.Abs(a.Y-cy) < 1e-9 {
					horizontal = append(horizontal, [2]float64{a.X, a.X})
				}
				if math.Abs(a.Y-cy) < 1e-9 && math.Abs(b.Y-cy) < 1e-9 {
					horizontal = append(horizontal, [2]float64{math.Min(a.X, b.X), math.Max(a.X, b.X)})
				}
				if (a.Y > cy) != (b.Y > cy) {
					crossings = append(crossings, a.X+(cy-a.Y)*(b.X-a.X)/(b.Y-a.Y))
				}
			}
		}
		sort.Float64s(crossings)
		for k := 1; k < len(crossings); k += 2 {
			left, right := crossings[k-1], crossings[k]
			begin := max(x0, int(math.Floor(left-.5+1e-9))+1)
			end := min(x1, int(math.Ceil(right-.5-1e-9))-1)
			for x := begin; x <= end; x++ {
				cx := float64(x) + .5
				onBoundary := false
				for _, span := range horizontal {
					if cx >= span[0]-1e-9 && cx <= span[1]+1e-9 {
						onBoundary = true
						break
					}
				}
				if onBoundary {
					continue
				}
				indices = append(indices, y*g.W+x)
				if len(indices) > maxEditCells {
					return nil, errors.New("selection exceeds 250,000 source cells; use a smaller area")
				}
			}
		}
	}
	if len(indices) == 0 {
		return nil, errors.New("waterbody contains no source cell centres")
	}
	return indices, nil
}
