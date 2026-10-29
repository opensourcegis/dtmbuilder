package main

import (
	"errors"
	"math"
	"sort"
)

// Select the union of circles swept along the stroke, including the segment
// between sparse pointer events. Cell centres define membership, just as for a
// single click. Visit each selected cell once, irrespective of overlapping passes.
func selectBrushCells(g *Grid, points []CellPoint, radius int) ([]int, map[int]bool, error) {
	r := float64(radius)
	indices := []int{}
	selected := map[int]bool{}
	for k := 0; k < max(1, len(points)-1); k++ {
		a, b := points[k], points[min(k+1, len(points)-1)]
		dx, dy := b.X-a.X, b.Y-a.Y
		length2 := dx*dx + dy*dy
		row0 := max(0, int(math.Ceil(math.Min(a.Y, b.Y)-r-.5)))
		row1 := min(g.H-1, int(math.Floor(math.Max(a.Y, b.Y)+r-.5)))
		for y := row0; y <= row1; y++ {
			cy := float64(y) + .5
			// A centre can only be near the portion of the segment whose Y is
			// within radius of this row. This keeps long diagonal strokes from
			// scanning their entire rectangular bounding box for every segment.
			t0, t1 := 0.0, 1.0
			if dy != 0 {
				u, v := (cy-r-a.Y)/dy, (cy+r-a.Y)/dy
				t0, t1 = math.Max(0, math.Min(u, v)), math.Min(1, math.Max(u, v))
				if t0 > t1 {
					continue
				}
			}
			xa, xb := a.X+t0*dx, a.X+t1*dx
			col0 := max(0, int(math.Ceil(math.Min(xa, xb)-r-.5)))
			col1 := min(g.W-1, int(math.Floor(math.Max(xa, xb)+r-.5)))
			for x := col0; x <= col1; x++ {
				i := y*g.W + x
				if selected[i] {
					continue
				}
				cx := float64(x) + .5
				t := 0.0
				if length2 > 0 {
					t = math.Max(0, math.Min(1, ((cx-a.X)*dx+(cy-a.Y)*dy)/length2))
				}
				xoff, yoff := cx-a.X-t*dx, cy-a.Y-t*dy
				if xoff*xoff+yoff*yoff > r*r {
					continue
				}
				selected[i] = true
				indices = append(indices, i)
				if len(indices) > maxEditCells {
					return nil, nil, errors.New("brush stroke exceeds 250,000 source cells; use shorter strokes")
				}
			}
		}
	}
	if len(indices) == 0 {
		return nil, nil, errors.New("brush contains no source cell centres")
	}
	sort.Ints(indices)
	return indices, selected, nil
}
