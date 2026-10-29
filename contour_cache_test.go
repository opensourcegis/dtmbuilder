package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"
)

func contourSegmentsForTest(c *ContourSet) []string {
	result := []string{}
	for _, line := range c.Lines {
		for i := 1; i < len(line.Points); i++ {
			a, b := line.Points[i-1], line.Points[i]
			sa, sb := fmt.Sprintf("%.6f,%.6f", a.X, a.Y), fmt.Sprintf("%.6f,%.6f", b.X, b.Y)
			if sa > sb {
				sa, sb = sb, sa
			}
			result = append(result, fmt.Sprintf("%.6f:%s:%s", line.Elevation, sa, sb))
		}
	}
	sort.Strings(result)
	return result
}
func TestTiledContoursMatchReferenceAndIncrementalHalos(t *testing.T) {
	g := &Grid{W: 271, H: 263, Z: make([]float32, 271*263)}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Z[y*g.W+x] = float32(50 + .02*float64(x) + math.Sin(float64(y)/23) + .1*math.Cos(float64(x)/11))
		}
	}
	for _, f := range []int{1, 2} {
		for _, r := range []int{0, 1, 5} {
			for _, regional := range []int{0, 2} {
				p := ContourParams{Interval: .5, Base: .13, IndexEvery: 5, AverageFactor: f, SmoothRadius: r, RegionalRadius: regional, ElevationQuantum: .03, PreserveHydro: true}
				z := append([]float32(nil), g.Z...)
				flags := make([]float32, len(z))
				z[10*g.W+10] = float32(math.NaN())
				full, cache, e := generateContoursCached(context.Background(), g, z, p, 0, func(int, string) {}, nil, cellRect{}, flags)
				if e != nil {
					t.Fatal(e)
				}
				ref, e := referenceContours(context.Background(), g, z, p, 0, func(int, string) {}, flags)
				if e != nil {
					t.Fatal(e)
				}
				if strings.Join(contourSegmentsForTest(full), "\n") != strings.Join(contourSegmentsForTest(ref), "\n") {
					t.Fatalf("full/reference mismatch f=%d r=%d reg=%d", f, r, regional)
				}
				old := append([]float32(nil), cache.Surface.Z...)
				for _, point := range [][2]int{{127, 128}, {0, 0}, {270, 262}} {
					x, y := point[0], point[1]
					z[y*g.W+x] += .81
					dirty := cellRect{x, y, x, y, true}
					incremental, next, e := generateContoursCached(context.Background(), g, z, p, 1, func(int, string) {}, cache, dirty, flags)
					if e != nil {
						t.Fatal(e)
					}
					expected, e := generateContours(context.Background(), g, z, p, 1, func(int, string) {}, flags)
					if e != nil {
						t.Fatal(e)
					}
					for i, v := range expected.Surface.Z {
						if valid(v) != valid(incremental.Surface.Z[i]) || valid(v) && math.Abs(float64(v-incremental.Surface.Z[i])) > 1e-5 {
							t.Fatalf("halo mismatch f=%d r=%d reg=%d cell=%d", f, r, regional, i)
						}
					}
					if strings.Join(contourSegmentsForTest(expected), "\n") != strings.Join(contourSegmentsForTest(incremental), "\n") {
						t.Fatalf("incremental geometry mismatch f=%d r=%d reg=%d point=%v", f, r, regional, point)
					}
					if incremental.Performance.Mode != "incremental" {
						t.Fatal("incremental cache was not used")
					}
					cache = next
				}
				for i, v := range old {
					if valid(v) && full.Surface.Z[i] != v {
						t.Fatal("cached surface mutated a published result")
					}
				}
			}
		}
	}
}
func waitForContours(t *testing.T, s *studio, revision int) *ContourSet {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		s.Lock()
		c, busy, failure := s.contours, s.contourBusy, s.failure
		s.Unlock()
		if c != nil && c.Revision == revision {
			return c
		}
		if !busy && failure != "" {
			t.Fatal(failure)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("contour refresh did not finish")
	return nil
}
func TestBackgroundContoursAllowEditsAndRejectObsoleteResults(t *testing.T) {
	g := &Grid{W: 520, H: 520, Z: make([]float32, 520*520)}
	for i := range g.Z {
		g.Z[i] = float32(i%g.W) * .01
	}
	s := &studio{grid: g, params: defaults()}
	s.result = initializeDTM(g)
	p := ContourParams{Interval: .5, IndexEvery: 5, AverageFactor: 1, SmoothRadius: 1}
	if e := s.startContourJob(p); e != nil {
		t.Fatal(e)
	}
	waitForContours(t, s, 0)
	s.Lock()
	if s.busy {
		t.Fatal("contour work blocked DTM editing")
	}
	if e := s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(250, 250, 255, 255), Elevation: 4, Revision: 0}); e != nil {
		t.Fatal(e)
	}
	s.Unlock()
	if e := s.startContourJob(p); e != nil {
		t.Fatal(e)
	}
	s.Lock()
	if e := s.applyEdit(EditRequest{Tool: "flatten", Points: rectangle(256, 256, 260, 260), Elevation: 4, Revision: 1}); e != nil {
		t.Fatal(e)
	}
	s.Unlock()
	if e := s.startContourJob(p); e != nil {
		t.Fatal(e)
	}
	c := waitForContours(t, s, 2)
	if c.Performance.Mode != "incremental" || c.Performance.TilesReused == 0 {
		t.Fatal("local DTM edits did not reuse unaffected contour tiles")
	}
	s.Lock()
	defer s.Unlock()
	if s.contours.Revision != s.edits.Revision {
		t.Fatal("an obsolete result was published")
	}
	expected := *s.result
	expected.Counts = map[string]int{}
	recount(&expected)
	for _, key := range []string{"manual", "hydro", "nonground", "nodata", "remainingNoData", "constraint"} {
		if s.result.Counts[key] != expected.Counts[key] {
			t.Fatalf("incremental edit counter %s differs", key)
		}
	}
}
func BenchmarkContourLocalEdit4Million(b *testing.B) {
	g := &Grid{W: 2048, H: 2048, Z: make([]float32, 2048*2048)}
	for i := range g.Z {
		g.Z[i] = float32(i%g.W)*.01 + float32(i/g.W)*.002
	}
	p := ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, SmoothRadius: 1}
	z := append([]float32(nil), g.Z...)
	_, cache, e := generateContoursCached(context.Background(), g, z, p, 0, func(int, string) {}, nil, cellRect{})
	if e != nil {
		b.Fatal(e)
	}
	dirty := cellRect{1020, 1020, 1028, 1028, true}
	for y := dirty.Y0; y <= dirty.Y1; y++ {
		for x := dirty.X0; x <= dirty.X1; x++ {
			z[y*g.W+x] += .1
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, e := generateContoursCached(context.Background(), g, z, p, 1, func(int, string) {}, cache, dirty); e != nil {
			b.Fatal(e)
		}
	}
}

func BenchmarkContoursFull4Million(b *testing.B) {
	g := &Grid{W: 2048, H: 2048, Z: make([]float32, 2048*2048)}
	for i := range g.Z {
		g.Z[i] = float32(i%g.W)*.01 + float32(i/g.W)*.002
	}
	p := ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, SmoothRadius: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, e := generateContoursCached(context.Background(), g, g.Z, p, 0, func(int, string) {}, nil, cellRect{}); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkContourLocalEdit16Million(b *testing.B) {
	g := &Grid{W: 4000, H: 4000, Z: make([]float32, 16000000)}
	for i := range g.Z {
		g.Z[i] = float32(i%g.W)*.01 + float32(i/g.W)*.002
	}
	p := ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, SmoothRadius: 1}
	z := append([]float32(nil), g.Z...)
	_, cache, e := generateContoursCached(context.Background(), g, z, p, 0, func(int, string) {}, nil, cellRect{})
	if e != nil {
		b.Fatal(e)
	}
	dirty := cellRect{1996, 1996, 2004, 2004, true}
	for y := dirty.Y0; y <= dirty.Y1; y++ {
		for x := dirty.X0; x <= dirty.X1; x++ {
			z[y*g.W+x] += .1
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, e := generateContoursCached(context.Background(), g, z, p, 1, func(int, string) {}, cache, dirty); e != nil {
			b.Fatal(e)
		}
	}
}
