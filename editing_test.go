package main

import (
	"math"
	"testing"
)

func editFixture() *studio {
	g := &Grid{W: 20, H: 20, Z: make([]float32, 400)}
	for i := range g.Z {
		g.Z[i] = 10
	}
	s := &studio{grid: g, params: defaults()}
	s.result = initializeDTM(g)
	return s
}
func rectangle(x0, y0, x1, y1 float64) []CellPoint {
	return []CellPoint{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}
func TestHydroflattenUndoRedo(t *testing.T) {
	s := editFixture()
	s.grid.Z[105] = float32(math.NaN())
	s.result = initializeDTM(s.grid)
	oldResult := s.result
	q := EditRequest{Tool: "flatten", Points: rectangle(4, 4, 8, 8), Elevation: 7, Revision: 0}
	if e := s.applyEdit(q); e != nil {
		t.Fatal(e)
	}
	if s.result.Counts["manual"] != 15 {
		t.Fatalf("changed cells: %v", s.result.Counts)
	}
	if valid(s.result.DTM[105]) {
		t.Fatal("NoData filled without explicit option")
	}
	if s.result.DTM[0] != 10 || s.result.DTM[84] != 7 || s.result.NDSM[84] != 3 {
		t.Fatal("wrong selected or derived values")
	}
	if oldResult.DTM[84] != 10 || s.grid.Z[84] != 10 {
		t.Fatal("source/export snapshot mutated")
	}
	if e := s.applyEdit(q); e == nil {
		t.Fatal("stale revision accepted")
	}
	if e := s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[84] != 10 || s.result.Counts["manual"] != 0 {
		t.Fatal("undo failed")
	}
	if e := s.historyEdit(true); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[84] != 7 {
		t.Fatal("redo failed")
	}
	q.FillNoData = true
	q.Revision = s.edits.Revision
	if e := s.applyEdit(q); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[105] != 7 || int(s.result.Flags[105])&240 != 240 {
		t.Fatal("manual NoData fill not flagged")
	}
	if e := s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if valid(s.result.DTM[105]) {
		t.Fatal("undo must restore NaN")
	}
	b, e := encodeTIFF(s.grid, s.result.DTM)
	if e != nil {
		t.Fatal(e)
	}
	g, e := decodeTIFF(b)
	if e != nil || g.Z[84] != 7 {
		t.Fatal("manual edits missing in export")
	}
}
func TestManualSpikeAndInterpolation(t *testing.T) {
	s := editFixture()
	s.grid.Z[210] = 1000
	s.result = initializeDTM(s.grid)
	if e := s.applyEdit(EditRequest{Tool: "spike", Points: []CellPoint{{10.5, 10.5}}, Radius: 2, Threshold: 1}); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[210] != 10 || s.result.DTM[0] != 10 {
		t.Fatal("spike repair failed")
	}
	if e := s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if e := s.applyEdit(EditRequest{Tool: "interpolate", Points: rectangle(9, 9, 12, 12), Revision: s.edits.Revision}); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[210] != 10 {
		t.Fatal("boundary interpolation failed")
	}
	if len(s.edits.Redo) != 0 {
		t.Fatal("new edit must clear redo")
	}
}
func TestEditValidationAndMask(t *testing.T) {
	s := editFixture()
	q := EditRequest{Tool: "flatten", Points: []CellPoint{{2, 2}, {8, 8}, {2, 8}, {8, 2}}, Elevation: 2}
	if e := s.applyEdit(q); e == nil {
		t.Fatal("self-intersecting polygon accepted")
	}
	q.Points = []CellPoint{{4, 4}, {9, 4}, {4, 9}}
	if e := s.applyEdit(q); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[4*20+4] != 2 || s.result.DTM[8*20+8] != 10 {
		t.Fatal("triangle selection applied outside geometry")
	}
	q.Revision = s.edits.Revision
	q.Elevation = math.NaN()
	if e := s.applyEdit(q); e == nil {
		t.Fatal("NaN height accepted")
	}
	s.busy = true
	if e := s.historyEdit(false); e == nil {
		t.Fatal("undo during processing accepted")
	}
}
