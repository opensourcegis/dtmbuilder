package main

import (
	"context"
	"math"
	"testing"
)

func TestContourDragConstrainsDTMAndSurvivesFilteringUndo(t *testing.T) {
	g := &Grid{W: 40, H: 40, Z: make([]float32, 1600)}
	for i := range g.Z {
		g.Z[i] = float32(i % 40)
	}
	s := &studio{grid: g, params: defaults()}
	s.result = initializeDTM(g)
	p := ContourParams{Interval: 5, Base: .25, IndexEvery: 5, AverageFactor: 1, SmoothRadius: 1, ElevationQuantum: .03, RegionalRadius: 1}
	c, e := generateContours(context.Background(), g, s.result.DTM, p, 0, func(int, string) {}, s.result.Flags)
	if e != nil {
		t.Fatal(e)
	}
	c.Generation = 9
	s.contours = c
	line, vertex := 2, 10
	target := c.Lines[line].Points[vertex]
	target.X += 2
	q := ContourPinRequest{Line: line, Vertex: vertex, X: target.X, Y: target.Y, Radius: 8, Generation: 9}
	if e = s.pinContour(q); e != nil {
		t.Fatal(e)
	}
	if len(s.constraints) != 1 || len(s.edits.Undo) != 1 {
		t.Fatal("pin missing from edit history")
	}
	pin := s.constraints[0]
	height, _, _, _, e := bilinearTerrain(g, s.result.DTM, target)
	if e != nil || math.Abs(height-pin.Elevation) > 1e-5 {
		t.Fatalf("DTM does not match the contour target: %g %g %v", height, pin.Elevation, e)
	}
	fresh, e := generateContours(context.Background(), g, s.result.DTM, p, 1, func(int, string) {}, s.result.Flags)
	if e != nil {
		t.Fatal(e)
	}
	height, _, _, _, e = bilinearTerrain(g, fresh.Surface.Z, target)
	if e != nil || math.Abs(height-pin.Elevation) > 1e-5 {
		t.Fatal("contour surface filtering changed a protected pin")
	}
	for _, i := range pin.Cells {
		if int(s.result.Flags[i])&256 == 0 {
			t.Fatal("pin anchor not protected")
		}
	}
	x, y := int(target.X), int(target.Y)
	before := s.result
	if e = s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(float64(x-2), float64(y-2), float64(x+2), float64(y+2)), Elevation: 100, Revision: 1}); e == nil {
		t.Fatal("conflicting terrain edit overwrote pin")
	}
	if s.result != before {
		t.Fatal("rejected conflicting edit made partial changes")
	}
	if e = s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if len(s.constraints) != 0 || s.result.DTM[pin.Cells[0]] != g.Z[pin.Cells[0]] {
		t.Fatal("Undo did not restore terrain and remove pin")
	}
	if e = s.historyEdit(true); e != nil {
		t.Fatal(e)
	}
	if len(s.constraints) != 1 {
		t.Fatal("Redo did not restore pin")
	}
	if e = s.removeContourPin(pin.ID, s.edits.Revision); e != nil {
		t.Fatal(e)
	}
	if len(s.constraints) != 0 || int(s.result.Flags[pin.Cells[0]])&256 != 0 {
		t.Fatal("remove pin failed")
	}
	if e = s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if len(s.constraints) != 1 || int(s.result.Flags[pin.Cells[0]])&256 == 0 {
		t.Fatal("Undo did not restore removed constraint")
	}
}
