package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"
)

func TestScanlineWaterSelectionMatchesGeometry(t *testing.T) {
	g := &Grid{W: 48, H: 48}
	fixtures := []TerrainFeature{
		{Points: rectangle(5.5, 5.5, 38.5, 38.5), Holes: [][]CellPoint{rectangle(12.5, 12.5, 18.5, 18.5)}},
		{Points: []CellPoint{{5, 5}, {40, 5}, {40, 40}, {20, 40}, {20, 25}, {5, 25}}, Holes: [][]CellPoint{rectangle(24, 11, 30, 18)}},
		{Points: []CellPoint{{7.25, 5.5}, {41.1, 10.2}, {35.4, 42.8}, {5.5, 32.5}}, Holes: [][]CellPoint{{{15.2, 12.5}, {26.5, 15.3}, {21.5, 26.5}, {12.5, 22.5}}}},
	}
	for _, f := range fixtures {
		if e := validateWaterRings(g, f.Points, f.Holes); e != nil {
			t.Fatal(e)
		}
		actual, e := selectWaterCells(g, f)
		if e != nil {
			t.Fatal(e)
		}
		selected := map[int]bool{}
		for _, i := range actual {
			if selected[i] {
				t.Fatal("duplicate scanline cell")
			}
			selected[i] = true
		}
		for y := range g.H {
			for x := range g.W {
				expected := inOpenWater(CellPoint{float64(x) + .5, float64(y) + .5}, f)
				if selected[y*g.W+x] != expected {
					t.Fatalf("scanline differs from geometric membership at (%d,%d)", x, y)
				}
			}
		}
	}
}
func TestWaterbodyRequestSupportsSixteenDetailedIslands(t *testing.T) {
	g := &Grid{W: 512, H: 512, Z: make([]float32, 512*512)}
	s := &studio{desktop: true, token: "test", grid: g, result: initializeDTM(g), params: defaults()}
	q := TerrainFeatureRequest{Kind: "waterbody", Points: rectangle(20.01, 20.02, 490.03, 490.04), Elevation: 10}
	for y := range 4 {
		for x := range 4 {
			ring := []CellPoint{}
			for k := range 256 {
				a := 2 * math.Pi * float64(k) / 256
				ring = append(ring, CellPoint{90.123 + 100*float64(x) + 15*math.Cos(a), 90.234 + 100*float64(y) + 15*math.Sin(a)})
			}
			q.Holes = append(q.Holes, ring)
		}
	}
	body, e := json.Marshal(q)
	if e != nil {
		t.Fatal(e)
	}
	if len(body) <= 128<<10 {
		t.Fatal("fixture does not exercise detailed-ring request sizing")
	}
	req := httptest.NewRequest("POST", "/api/terrain-features", bytes.NewReader(body))
	req.Header.Set("X-Studio-Token", "test")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 || len(s.terrainFeatures) != 1 || len(s.terrainFeatures[0].Holes) != 16 {
		t.Fatalf("detailed waterbody rejected: %d %s", w.Code, w.Body.String())
	}
}
