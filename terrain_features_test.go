package main

import (
	"context"
	"math"
	"strings"
	"testing"
)

func terrainFeatureFixture() *studio {
	g := &Grid{W: 48, H: 48, Z: make([]float32, 48*48)}
	for y := range g.H {
		for x := range g.W {
			g.Z[y*g.W+x] = float32(20 + .1*float64(x) + .02*float64(y))
		}
	}
	return &studio{grid: g, result: initializeDTM(g), params: defaults()}
}
func TestDrawnBreaklineGradeProtectionAndHistory(t *testing.T) {
	s := terrainFeatureFixture()
	before := append([]float32{}, s.result.DTM...)
	q := TerrainFeatureRequest{Kind: "breakline", Mode: "stream", Points: []CellPoint{{10.5, 10.5}, {30.5, 10.5}}, Radius: 1, Elevation: 25, EndElevation: 21}
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	if len(s.terrainFeatures) != 1 || s.result.DTM[10*48+10] != 25 || s.result.DTM[10*48+30] != 21 || s.result.DTM[10*48+20] != 23 {
		t.Fatal("endpoint grade not applied along the drawn line")
	}
	if s.result.DTM[20*48+20] != before[20*48+20] {
		t.Fatal("breakline modified distant terrain")
	}
	result := s.result
	if e := s.applyEdit(EditRequest{Tool: "flatten", Revision: 1, Points: rectangle(18, 8, 23, 13), Elevation: 30}); e == nil || s.result != result {
		t.Fatal("conflicting edit was not rejected atomically")
	}
	p := ContourParams{Interval: 1, AverageFactor: 1, IndexEvery: 5, EdgePasses: 5, EdgeThreshold: .2, ElevationQuantum: .03048, MaxSurfaceChange: .1}
	c, e := generateContours(context.Background(), s.grid, s.result.DTM, p, 1, func(int, string) {}, s.result.Flags)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range s.terrainFeatures[0].Cells {
		if c.Surface.Z[i] != s.result.DTM[i] {
			t.Fatal("filter changed a protected breakline sample")
		}
	}
	if e = s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	if len(s.terrainFeatures) != 0 || s.result.DTM[10*48+20] != before[10*48+20] {
		t.Fatal("Undo lost pre-breakline terrain")
	}
	if e = s.historyEdit(true); e != nil {
		t.Fatal(e)
	}
	f := s.terrainFeatures[0]
	if e = s.removeTerrainFeature(f.ID, s.edits.Revision); e != nil {
		t.Fatal(e)
	}
	if len(s.terrainFeatures) != 0 || int(s.result.Flags[10*48+20])&512 != 0 || s.result.DTM[10*48+20] != 23 {
		t.Fatal("removing a breakline did not retain heights and release samples")
	}
	if e = s.historyEdit(false); e != nil || len(s.terrainFeatures) != 1 {
		t.Fatal("Undo did not restore a removed feature")
	}
}
func TestWaterbodyIslandsConflictsAndGeometryValidation(t *testing.T) {
	s := terrainFeatureFixture()
	s.result.DTM[9*48+9] = float32(math.NaN())
	island := s.result.DTM[18*48+18]
	q := TerrainFeatureRequest{Kind: "waterbody", Points: rectangle(8, 8, 30, 30), Holes: [][]CellPoint{rectangle(16, 16, 21, 21)}, Elevation: 22}
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	if s.result.DTM[12*48+12] != 22 || s.result.DTM[18*48+18] != island || valid(s.result.DTM[9*48+9]) {
		t.Fatal("waterbody did not preserve islands/NoData")
	}
	if int(s.result.Flags[12*48+12])&(128|1024) != (128|1024) || int(s.result.Flags[18*48+18])&1024 != 0 {
		t.Fatal("incorrect water locking")
	}
	original := s.result
	q.Revision = 1
	q.Elevation = 23
	if e := s.applyTerrainFeature(q); e == nil || s.result != original {
		t.Fatal("different overlapping water levels must conflict atomically")
	}
	q.Elevation = 22
	q.Holes = [][]CellPoint{rectangle(7, 7, 12, 12)}
	if e := s.applyTerrainFeature(q); e == nil {
		t.Fatal("island outside the waterbody accepted")
	}
	q.Holes = [][]CellPoint{rectangle(16, 16, 21, 21), rectangle(18, 18, 23, 23)}
	if e := s.applyTerrainFeature(q); e == nil {
		t.Fatal("overlapping islands accepted")
	}
	q.Holes = nil
	q.Points = []CellPoint{{8, 8}, {25, 25}, {8, 25}, {25, 8}}
	if e := s.applyTerrainFeature(q); e == nil {
		t.Fatal("self-crossing water polygon accepted")
	}
	for _, p := range []ContourParams{{Interval: 1, AverageFactor: 2, IndexEvery: 5}, {Interval: 1, AverageFactor: 1, IndexEvery: 5, SmoothRadius: 1}, {Interval: 1, AverageFactor: 1, IndexEvery: 5, RegionalRadius: 1}} {
		if _, e := contourSurface(context.Background(), s.grid, s.result.DTM, p, s.result.Flags); e == nil {
			t.Fatal("a mixing filter was permitted across hard feature constraints")
		}
	}
}
func TestBreaklinePreserveNoDataCrossingAndSharedOwnership(t *testing.T) {
	s := terrainFeatureFixture()
	q := TerrainFeatureRequest{Kind: "breakline", Mode: "preserve", Points: []CellPoint{{10.5, 10.5}, {30.5, 10.5}}, Radius: 1}
	before := append([]float32{}, s.result.DTM...)
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	for i, v := range before {
		if v != s.result.DTM[i] {
			t.Fatal("preserve mode changed DTM heights")
		}
	}
	q.Revision = 1
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	first := s.terrainFeatures[0].ID
	if e := s.removeTerrainFeature(first, 2); e != nil || int(s.result.Flags[10*48+20])&512 == 0 {
		t.Fatal("removal released another feature's shared cells")
	}
	bad := q
	bad.Revision = s.edits.Revision
	bad.Mode = "stream"
	bad.Elevation = 20
	bad.EndElevation = 21
	if e := s.applyTerrainFeature(bad); e == nil {
		t.Fatal("uphill stream endpoint profile accepted")
	}
	bad = q
	bad.Revision = s.edits.Revision
	bad.Points = []CellPoint{{5, 5}, {30, 30}, {5, 30}, {30, 5}}
	if e := s.applyTerrainFeature(bad); e == nil {
		t.Fatal("self-crossing breakline accepted")
	}
	s = terrainFeatureFixture()
	s.result.DTM[10*48+20] = float32(math.NaN())
	snapshot := s.result
	q.Revision = 0
	if e := s.applyTerrainFeature(q); e == nil || !strings.Contains(e.Error(), "NoData") || s.result != snapshot {
		t.Fatal("NoData gap did not reject preserve mode atomically")
	}
}
func TestWaterContourClippingPreservesIslandsAndBoundary(t *testing.T) {
	g := &Grid{W: 48, H: 48}
	f := TerrainFeature{Kind: "waterbody", Points: rectangle(10, 10, 30, 30), Holes: [][]CellPoint{rectangle(18, 18, 22, 22)}}
	c := &ContourSet{Lines: []ContourLine{{Elevation: 22, Points: []CellPoint{{0, 20}, {40, 20}}}, {Elevation: 22, Points: []CellPoint{{10, 10}, {30, 10}}}}}
	if e := clipWaterContours(context.Background(), g, c, []TerrainFeature{f}); e != nil {
		t.Fatal(e)
	}
	if len(c.Lines) != 4 {
		t.Fatalf("expected outside/island/outside/boundary pieces, got %d", len(c.Lines))
	}
	expected := [][2]float64{{0, 10}, {18, 22}, {30, 40}, {10, 30}}
	for i, line := range c.Lines {
		if line.Points[0].X != expected[i][0] || line.Points[len(line.Points)-1].X != expected[i][1] {
			t.Fatalf("incorrect clipping endpoint: %+v", line.Points)
		}
		for j := 1; j < len(line.Points); j++ {
			a, b := line.Points[j-1], line.Points[j]
			if inOpenWater(CellPoint{(a.X + b.X) / 2, (a.Y + b.Y) / 2}, f) {
				t.Fatal("a contour remains inside water")
			}
		}
	}
	if c.WaterClippedSegments != 1 {
		t.Fatal("clipping audit missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := clipWaterContours(ctx, g, c, []TerrainFeature{f}); e == nil {
		t.Fatal("cancelled clipping succeeded")
	}
}

func TestTerrainFeatureAuditPayloadAndHistoryBranch(t *testing.T) {
	s := terrainFeatureFixture()
	q := TerrainFeatureRequest{Kind: "waterbody", Points: rectangle(8, 8, 30, 30), Elevation: 24}
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	for _, audit := range s.edits.Audit {
		if audit.FeaturesBefore != nil || audit.FeaturesAfter != nil || audit.ConstraintsBefore != nil || audit.ConstraintsAfter != nil {
			t.Fatal("audit retained hidden history snapshots")
		}
	}
	p := ContourParams{Interval: 1, AverageFactor: 1, IndexEvery: 5, EdgePasses: 2, EdgeThreshold: .2}
	if e := s.startContourJob(p); e != nil {
		t.Fatal(e)
	}
	c := waitForContours(t, s, 1)
	if c.Quality.ShorelineBelowLevelCells == 0 {
		t.Fatal("inconsistent low banks were not flagged")
	}
	if e := s.historyEdit(false); e != nil {
		t.Fatal(e)
	}
	q.Revision = s.edits.Revision
	q.Elevation = 23
	if e := s.applyTerrainFeature(q); e != nil {
		t.Fatal(e)
	}
	if len(s.edits.Redo) != 0 || s.edits.Bytes != editRecordBytes(s.edits.Undo[0]) {
		t.Fatal("branched history retained discarded Redo bytes")
	}
	// Hard constraints in cropped averaging margins must never be silently lost.
	g := &Grid{W: 5, H: 5, Z: make([]float32, 25)}
	flags := make([]float32, 25)
	flags[24] = 512
	p.AverageFactor = 2
	if _, e := contourSurface(context.Background(), g, g.Z, p, flags); e == nil {
		t.Fatal("averaging silently cropped a protected edge sample")
	}
}
