package main

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestEdgeFilterNoiseReliefNoDataAndChangeBound(t *testing.T) {
	w, h := 64, 64
	g := &Grid{W: w, H: h, Z: make([]float32, w*h)}
	for y := range h {
		for x := range w {
			v := 50.0
			if x >= 32 {
				v += 5
			}
			if (x+y)%2 == 0 {
				v += .05
			} else {
				v -= .05
			}
			g.Z[y*w+x] = float32(v)
		}
	}
	g.Z[2*w+2] = float32(math.NaN())
	p := ContourParams{Interval: 1, AverageFactor: 1, IndexEvery: 5, EdgePasses: 5, EdgeThreshold: .3, MaxSurfaceChange: .04}
	surface, e := contourSurface(context.Background(), g, g.Z, p)
	if e != nil {
		t.Fatal(e)
	}
	before, after := 0.0, 0.0
	for y := 8; y < 56; y++ {
		for x := 8; x < 24; x++ {
			i := y*w + x
			before += math.Pow(float64(g.Z[i])-50, 2)
			after += math.Pow(float64(surface.Z[i])-50, 2)
		}
	}
	if after >= before*.25 {
		t.Fatal("edge filter failed to reduce low-amplitude plateau noise")
	}
	if float64(surface.Z[30*w+32]-surface.Z[30*w+31]) < 4.8 {
		t.Fatal("edge filter destroyed the sharp bank")
	}
	if valid(surface.Z[2*w+2]) {
		t.Fatal("NoData was filled by smoothing")
	}
	rawParams := p
	rawParams.EdgePasses = 0
	rawParams.MaxSurfaceChange = 0
	rawParams.Base = 50.025
	filteredParams := p
	filteredParams.Base = 50.025
	raw, e := generateContours(context.Background(), g, g.Z, rawParams, 0, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	filtered, e := generateContours(context.Background(), g, g.Z, filteredParams, 0, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(filtered.Lines) >= len(raw.Lines)/4 {
		t.Fatalf("tiny plateau contours were not reduced without size filtering: raw %d filtered %d", len(raw.Lines), len(filtered.Lines))
	}
	for i, v := range surface.Z {
		if valid(v) && math.Abs(float64(v)-float64(g.Z[i])) > p.MaxSurfaceChange {
			t.Fatal("smoothing exceeded the declared change bound")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = contourSurface(ctx, g, g.Z, p); e == nil {
		t.Fatal("cancelled filter succeeded")
	}
}
func TestEdgeFilterIncrementalHalosAndProtectedBarrier(t *testing.T) {
	w, h := 271, 263
	g := &Grid{W: w, H: h, Z: make([]float32, w*h)}
	flags := make([]float32, w*h)
	for y := range h {
		for x := range w {
			g.Z[y*w+x] = float32(30 + .02*float64(x) + .1*math.Sin(float64(y)))
			if x == 130 {
				flags[y*w+x] = 512
			}
		}
	}
	p := ContourParams{Interval: .5, Base: .13, AverageFactor: 1, IndexEvery: 5, EdgePasses: 5, EdgeThreshold: .2, MaxSurfaceChange: .1}
	z := append([]float32{}, g.Z...)
	_, prior, e := generateContoursCached(context.Background(), g, z, p, 0, func(int, string) {}, nil, cellRect{}, flags)
	if e != nil {
		t.Fatal(e)
	}
	for _, point := range [][2]int{{127, 128}, {0, 0}, {270, 262}} {
		x, y := point[0], point[1]
		z[y*w+x] += .2
		actual, cache, e := generateContoursCached(context.Background(), g, z, p, 1, func(int, string) {}, prior, cellRect{x, y, x, y, true}, flags)
		if e != nil {
			t.Fatal(e)
		}
		expected, e := generateContours(context.Background(), g, z, p, 1, func(int, string) {}, flags)
		if e != nil {
			t.Fatal(e)
		}
		for i, v := range expected.Surface.Z {
			if actual.Surface.Z[i] != v {
				t.Fatalf("edge-filter halo differs at %d", i)
			}
		}
		if strings.Join(contourSegmentsForTest(actual), "\n") != strings.Join(contourSegmentsForTest(expected), "\n") {
			t.Fatal("incremental edge-filter contours differ from full output")
		}
		prior = cache
	}
	// Alter one side only: fixed line samples prevent influence crossing the barrier.
	original := append([]float32{}, g.Z...)
	modified := append([]float32{}, original...)
	for y := range h {
		modified[y*w+129] += 2
	}
	a, e := contourSurface(context.Background(), g, original, p, flags)
	if e != nil {
		t.Fatal(e)
	}
	b, e := contourSurface(context.Background(), g, modified, p, flags)
	if e != nil {
		t.Fatal(e)
	}
	for y := range h {
		for x := 130; x < w; x++ {
			if a.Z[y*w+x] != b.Z[y*w+x] {
				t.Fatal("filter influence crossed a protected breakline")
			}
		}
	}
}

func benchmarkConstrainedContours(b *testing.B, size int, local bool) {
	g := &Grid{W: size, H: size, Z: make([]float32, size*size)}
	for i := range g.Z {
		g.Z[i] = float32(30 + .01*float64(i%size) + .002*float64(i/size))
	}
	z := append([]float32{}, g.Z...)
	flags := make([]float32, len(z))
	for y := range size {
		flags[y*size+size/2] = 512
	}
	x0, y0 := size*2/3, size/2
	side := min(300, size/4)
	water := TerrainFeature{Kind: "waterbody", Points: rectangle(float64(x0), float64(y0), float64(x0+side), float64(y0+side)), Elevation: float64(z[(y0+side/2)*size+x0+side/2])}
	for y := y0; y < y0+side; y++ {
		for x := x0; x < x0+side; x++ {
			i := y*size + x
			z[i] = float32(water.Elevation)
			flags[i] = 1024 | 128
			water.Cells = append(water.Cells, i)
		}
	}
	p := ContourParams{Interval: 1, AverageFactor: 1, IndexEvery: 5, EdgePasses: 4, EdgeThreshold: .5, MaxSurfaceChange: .2, PreserveHydro: true}
	var prior *contourCache
	var dirty cellRect
	if local {
		_, cache, e := generateContoursCached(context.Background(), g, z, p, 0, func(int, string) {}, nil, cellRect{}, flags)
		if e != nil {
			b.Fatal(e)
		}
		prior = cache
		dirty = cellRect{200, 200, 208, 208, true}
		for y := dirty.Y0; y <= dirty.Y1; y++ {
			for x := dirty.X0; x <= dirty.X1; x++ {
				z[y*size+x] += .1
			}
		}
	}
	b.ResetTimer()
	for range b.N {
		c, _, e := generateContoursCached(context.Background(), g, z, p, 1, func(int, string) {}, prior, dirty, flags)
		if e != nil {
			b.Fatal(e)
		}
		if e = clipWaterContours(context.Background(), g, c, []TerrainFeature{water}); e != nil {
			b.Fatal(e)
		}
		if e = assessWaterBanks(context.Background(), g, z, flags, c, []TerrainFeature{water}); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkConstrainedContoursFull4Million(b *testing.B) {
	benchmarkConstrainedContours(b, 2048, false)
}
func BenchmarkConstrainedContoursLocal4Million(b *testing.B) {
	benchmarkConstrainedContours(b, 2048, true)
}
func BenchmarkConstrainedContoursLocal16Million(b *testing.B) {
	benchmarkConstrainedContours(b, 4000, true)
}
