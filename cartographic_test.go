package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

func cartographicParams() ContourParams {
	return ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, ElevationQuantum: .1, ElevationUnit: "m", SmoothRadius: 1, PreserveHydro: true}
}

func TestCartographicRoundThenFilter(t *testing.T) {
	g := &Grid{W: 3, H: 3, Z: []float32{.044, .044, .044, .044, .149, .044, .044, .044, .044}}
	p := cartographicParams()
	s, err := contourSurface(context.Background(), g, g.Z, p)
	if err != nil {
		t.Fatal(err)
	}
	// Round first: eight zeros and one 0.1, then a 3x3 mean. Filtering
	// first would give 0.0557 and rounding last would give 0.1 instead.
	if math.Abs(float64(s.Z[4])-.1/9) > 1e-8 {
		t.Fatalf("wrong operation order: %g", s.Z[4])
	}
	if g.Z[4] != float32(.149) || g.Z[0] != float32(.044) {
		t.Fatal("source DTM was changed")
	}
	g.Z[0] = float32(math.NaN())
	s, err = contourSurface(context.Background(), g, g.Z, p)
	if err != nil {
		t.Fatal(err)
	}
	if valid(s.Z[0]) || math.Abs(float64(s.Z[4])-.1/8) > 1e-8 {
		t.Fatal("NoData was filled or used as zero")
	}
}

func TestRoundingRemovesPlateauNoiseWithoutLoopFiltering(t *testing.T) {
	g := &Grid{W: 21, H: 21, Z: make([]float32, 21*21)}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Z[y*g.W+x] = float32(10 + .006*math.Sin(float64(x)*2)*math.Cos(float64(y)*2))
		}
	}
	p := cartographicParams()
	p.ElevationQuantum = 0
	p.SmoothRadius = 0
	raw, err := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Lines) == 0 {
		t.Fatal("fixture did not exercise plateau contours")
	}
	p.ElevationQuantum = .1
	p.SmoothRadius = 1
	clean, err := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Lines) != 0 || clean.RemovedSmallLoops != 0 {
		t.Fatal("plateau contours should disappear through surface treatment, not loop deletion")
	}
	// A real hill remains visible at meaningful levels after the same treatment.
	for y := 7; y <= 13; y++ {
		for x := 7; x <= 13; x++ {
			g.Z[y*g.W+x] += float32(math.Max(0, 3-math.Hypot(float64(x-10), float64(y-10))))
		}
	}
	p.Base = .5
	hill, err := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil || len(hill.Lines) == 0 {
		t.Fatalf("meaningful hill was lost: %v", err)
	}
}

func TestProtectedWaterSurvivesQuantizationAndAllFilters(t *testing.T) {
	s := editFixture()
	if err := s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(4, 4, 10, 10), Elevation: 7.13}); err != nil {
		t.Fatal(err)
	}
	p := cartographicParams()
	p.RegionalRadius = 2
	c, err := generateContours(context.Background(), s.grid, s.result.DTM, p, s.edits.Revision, func(int, string) {}, s.result.Flags)
	if err != nil {
		t.Fatal(err)
	}
	if c.Quality.PreservedHydroBlocks != 36 {
		t.Fatalf("lost water mask: %+v", c.Quality)
	}
	for y := 4; y < 10; y++ {
		for x := 4; x < 10; x++ {
			if c.Surface.Z[y*20+x] != float32(7.13) {
				t.Fatal("known water level changed")
			}
		}
	}
	for _, line := range c.Lines {
		for _, pt := range line.Points {
			if pt.X > 4.5 && pt.X < 9.5 && pt.Y > 4.5 && pt.Y < 9.5 {
				t.Fatal("contour enters the protected flat-water interior")
			}
		}
	}
	unprotected := p
	unprotected.PreserveHydro = false
	free, err := contourSurface(context.Background(), s.grid, s.result.DTM, unprotected, s.result.Flags)
	if err != nil || free.Z[4*20+4] == float32(7.13) {
		t.Fatalf("disabling water protection did not restore normal filtering: %v", err)
	}
	p.AverageFactor = 2
	c, err = generateContours(context.Background(), s.grid, s.result.DTM, p, s.edits.Revision, func(int, string) {}, s.result.Flags)
	if err != nil || c.Quality.PreservedHydroBlocks != 9 {
		t.Fatalf("water lost during averaging: %v", err)
	}
	if c.Surface.Z[2*10+2] != float32(7.13) {
		t.Fatal("averaging changed water level")
	}
	s.result.DTM[4*20+5] = 8
	if _, err = contourSurface(context.Background(), s.grid, s.result.DTM, p, s.result.Flags); err == nil {
		t.Fatal("different water levels were silently averaged")
	}
}

func TestWaterConstraintWithoutElevationChangeAndReplacement(t *testing.T) {
	s := editFixture()
	if err := s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(4, 4, 8, 8), Elevation: 10}); err != nil {
		t.Fatal(err)
	}
	if s.result.Counts["hydro"] != 16 {
		t.Fatal("already-level cells must still acquire a water constraint")
	}
	if err := s.historyEdit(false); err != nil {
		t.Fatal(err)
	}
	if s.result.Counts["hydro"] != 0 || s.result.DTM[84] != 10 {
		t.Fatal("constraint undo failed")
	}
	if err := s.historyEdit(true); err != nil {
		t.Fatal(err)
	}
	q := EditRequest{Tool: "flatten", Points: rectangle(4, 4, 8, 8), Elevation: 7, Revision: s.edits.Revision}
	if err := s.applyEdit(q); err != nil {
		t.Fatal(err)
	}
	if err := s.applyEdit(EditRequest{Tool: "smooth", Points: rectangle(4, 4, 8, 8), Revision: s.edits.Revision}); err != nil {
		t.Fatal(err)
	}
	if int(s.result.Flags[84])&128 != 0 {
		t.Fatal("a later height-changing edit kept a stale water constraint")
	}
	if err := s.historyEdit(false); err != nil {
		t.Fatal(err)
	}
	if int(s.result.Flags[84])&128 == 0 || s.result.DTM[84] != 7 {
		t.Fatal("undo did not restore water elevation and constraint")
	}
}

func TestContourQualityCoverageAndChange(t *testing.T) {
	g := &Grid{W: 5, H: 5, Z: make([]float32, 25)}
	for i := range g.Z {
		g.Z[i] = 3
	}
	p := cartographicParams()
	p.AverageFactor = 2
	p.ElevationQuantum = 0
	p.SmoothRadius = 0
	c, err := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if c.Quality.ComparedCells != 16 || c.Quality.CroppedSourceCells != 9 || c.Quality.RMSEChange != 0 {
		t.Fatalf("incorrect quality counts: %+v", c.Quality)
	}
	g.Z[0] = float32(math.NaN())
	c, err = generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if c.Quality.MaskedSourceCells != 3 || c.Quality.ComparedCells != 12 {
		t.Fatalf("NoData blocks not reported: %+v", c.Quality)
	}
	for i := range g.Z {
		g.Z[i] = .04
	}
	p.AverageFactor = 1
	p.ElevationQuantum = .1
	c, err = generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(c.Quality.RMSEChange-.04) > 1e-7 || math.Abs(c.Quality.MeanChange+.04) > 1e-7 || c.Quality.ChangedCells != 25 {
		t.Fatalf("incorrect change measurements: %+v", c.Quality)
	}
}

func TestCartographicValidationAndEmptySurface(t *testing.T) {
	g := contourPlane()
	p := cartographicParams()
	for _, q := range []float64{-1, math.NaN(), math.Inf(1), 1e-320} {
		p.ElevationQuantum = q
		if _, err := contourSurface(context.Background(), g, g.Z, p); err == nil {
			t.Fatalf("invalid quantum accepted: %g", q)
		}
	}
	p = cartographicParams()
	p.ElevationUnit = "degrees"
	if err := p.validate(); err == nil {
		t.Fatal("invalid elevation units accepted")
	}
	p = cartographicParams()
	if _, err := contourSurface(context.Background(), g, g.Z, p, []float32{128}); err == nil {
		t.Fatal("mismatched constraints accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := contourSurface(ctx, g, g.Z, p); err == nil {
		t.Fatal("cancellation ignored")
	}
	for i := range g.Z {
		g.Z[i] = float32(math.NaN())
	}
	if _, err := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {}); err == nil {
		t.Fatal("empty surface reported as a successful contour run")
	}
}

func TestContourQualityReportsAndInvalidation(t *testing.T) {
	s := editFixture()
	s.desktop = true
	s.token = "test"
	c, err := generateContours(context.Background(), s.grid, s.result.DTM, cartographicParams(), 0, func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	s.contours = c
	for _, path := range []string{"/api/contours/report", "/api/report", "/api/state"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Studio-Token", "test")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "rmseChange") {
			t.Fatalf("quality missing from %s: %s", path, w.Body.String())
		}
		var report map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(4, 4, 8, 8), Elevation: 7}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/contours/report", nil)
	req.Header.Set("X-Studio-Token", "test")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("old contour quality report survived a terrain change")
	}
}
