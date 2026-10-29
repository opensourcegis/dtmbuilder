package main

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBrushSweepsBetweenPointsAndKeepsCircularEnds(t *testing.T) {
	g := &Grid{W: 30, H: 25}
	q := EditRequest{Tool: "spike", Radius: 2, Points: []CellPoint{{5.5, 10.5}, {20.5, 10.5}}}
	indices, selected, err := selectCells(g, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range [][2]int{{3, 10}, {5, 8}, {12, 8}, {12, 12}, {20, 10}, {22, 10}} {
		if !selected[point[1]*g.W+point[0]] {
			t.Fatalf("brush missed covered cell %v", point)
		}
	}
	for _, point := range [][2]int{{3, 8}, {22, 12}, {12, 7}, {12, 13}, {23, 10}} {
		if selected[point[1]*g.W+point[0]] {
			t.Fatalf("brush selected outside cell %v", point)
		}
	}
	q.Points = []CellPoint{{20.5, 10.5}, {5.5, 10.5}, {20.5, 10.5}, {20.5, 10.5}}
	retraced, _, err := selectCells(g, q)
	if err != nil || !reflect.DeepEqual(indices, retraced) {
		t.Fatal("overlap/retracing changed brush footprint or repeated cells")
	}
	// The diagonal's midway cell belongs to the swept path even when neither
	// endpoint circle covers it. Its offset neighbour lies outside radius 1.
	q.Radius = 1
	q.Points = []CellPoint{{2.5, 2.5}, {20.5, 20.5}}
	_, selected, err = selectCells(g, q)
	if err != nil || !selected[11*g.W+11] || selected[14*g.W+11] {
		t.Fatal("diagonal brush coverage is incorrect")
	}
	q.Points = []CellPoint{{0, 0}}
	_, selected, err = selectCells(g, q)
	if err != nil || len(selected) != 1 || !selected[0] {
		t.Fatal("edge brush was not clipped to valid cell centres")
	}
}

func TestSpikeStrokeRepairsWholeAreaAsOneUndo(t *testing.T) {
	s := editFixture()
	high, low, outside, nodata := 10*20+8, 11*20+12, 14*20+8, 9*20+10
	s.grid.Z[high] = 90
	s.grid.Z[low] = -40
	s.grid.Z[outside] = 100
	s.grid.Z[nodata] = float32(math.NaN())
	s.result = initializeDTM(s.grid)
	before := s.result
	q := EditRequest{Tool: "spike", Radius: 2, Threshold: 1, FillNoData: true, Points: []CellPoint{{4.5, 10.5}, {15.5, 10.5}, {4.5, 10.5}}}
	if err := s.applyEdit(q); err != nil {
		t.Fatal(err)
	}
	if s.result.DTM[high] != 10 || s.result.DTM[low] != 10 {
		t.Fatal("high/low spikes away from stroke endpoints were not repaired")
	}
	if s.result.DTM[outside] != 100 || valid(s.result.DTM[nodata]) {
		t.Fatal("brush changed outside cells or filled NoData")
	}
	if s.grid.Z[high] != 90 || before.DTM[high] != 90 || s.result.Clean[high] != 90 {
		t.Fatal("original elevations or pre-stroke snapshot changed")
	}
	if len(s.edits.Undo) != 1 || s.edits.Undo[0].Cells != 2 || len(s.edits.Audit) != 1 {
		t.Fatal("overlapping stroke must commit only two cell changes as one action")
	}
	if len(s.edits.Audit[0].Points) != 3 || s.edits.Audit[0].Radius != 2 {
		t.Fatal("stroke geometry missing from audit")
	}
	if s.result.NDSM[high] != 80 || s.result.NDSM[low] != 0 || int(s.result.Flags[high])&64 == 0 {
		t.Fatal("derived heights or flags not updated")
	}
	if err := s.historyEdit(false); err != nil {
		t.Fatal(err)
	}
	if s.result.DTM[high] != 90 || s.result.DTM[low] != -40 || s.result.Flags[high] != before.Flags[high] {
		t.Fatal("one Undo did not restore the full stroke")
	}
	if err := s.historyEdit(true); err != nil {
		t.Fatal(err)
	}
	if s.result.DTM[high] != 10 || s.result.DTM[low] != 10 {
		t.Fatal("one Redo did not restore the full repair")
	}
}

func TestSpikeBrushThresholdAndNoop(t *testing.T) {
	s := editFixture()
	s.grid.Z[210] = 10.5
	s.result = initializeDTM(s.grid)
	q := EditRequest{Tool: "spike", Radius: 3, Threshold: 1, Points: []CellPoint{{9.5, 10.5}, {12.5, 10.5}}}
	s.failure = "previous processing error"
	if err := s.applyEdit(q); err != nil {
		t.Fatal(err)
	}
	if len(s.edits.Undo) != 0 || s.edits.Revision != 0 || s.result.DTM[210] != 10.5 {
		t.Fatal("threshold/no-op created an edit")
	}
	if !strings.Contains(s.message, "brush area") || s.failure != "" {
		t.Fatal("no-op did not report a successful brush check")
	}
	q.Threshold = 0
	if err := s.applyEdit(q); err != nil || s.result.DTM[210] != 10 {
		t.Fatalf("zero threshold did not repair a differing sample: %v", err)
	}
}

func TestBrushValidationAndWorkLimits(t *testing.T) {
	g := &Grid{W: 30, H: 30}
	for _, q := range []EditRequest{
		{Tool: "spike", Radius: 0, Points: []CellPoint{{5, 5}}},
		{Tool: "spike", Radius: 51, Points: []CellPoint{{5, 5}}},
		{Tool: "spike", Radius: 1, Points: []CellPoint{{-1, 5}}},
		{Tool: "spike", Radius: 1, Points: []CellPoint{{math.NaN(), 5}}},
		{Tool: "spike", Radius: 1, Points: make([]CellPoint, 257)},
	} {
		if _, _, err := selectCells(g, q); err == nil {
			t.Fatalf("invalid brush accepted: %+v", q)
		}
	}
	g = &Grid{W: 3000, H: 200}
	if _, _, err := selectCells(g, EditRequest{Tool: "spike", Radius: 50, Points: []CellPoint{{100, 100}, {2800, 100}}}); err == nil {
		t.Fatal("brush cell limit was ignored")
	}
	g = &Grid{W: 2000, H: 2000}
	if indices, _, err := selectCells(g, EditRequest{Tool: "spike", Radius: 1, Points: []CellPoint{{1, 1}, {1999, 1999}}}); err != nil || len(indices) == 0 || len(indices) > 10000 {
		t.Fatal("a narrow diagonal brush should work without scanning its full bounding box")
	}
}
