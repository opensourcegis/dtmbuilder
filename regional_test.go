package main

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func TestRegionalLengthAndPreviewAlignment(t *testing.T) {
	g := &Grid{W: 7, H: 7, Z: make([]float32, 49), Order: binary.LittleEndian, Tags: map[uint16]tag{}}
	for i := range g.Z {
		g.Z[i] = 100
	}
	put := func(id uint16, v []float64) {
		b := make([]byte, len(v)*8)
		for i, n := range v {
			g.Order.PutUint64(b[i*8:], math.Float64bits(n))
		}
		g.Tags[id] = tag{id, 12, uint32(len(v)), b}
	}
	put(33550, []float64{.1, .2, 0})
	put(33922, []float64{0, 0, 0, 500, 1000, 0})
	line := ContourLine{Points: []CellPoint{{0, 0}, {5, 10}}}
	if math.Abs(contourLength(g, line)-math.Hypot(.5, 2)) > 1e-8 {
		t.Fatal("line length ignored native grid spacing")
	}
	p := defaultContourParams()
	p.MinArea = 0
	p.AverageFactor = 2
	p.SmoothRadius = 0
	s, e := contourSurface(context.Background(), g, g.Z, p)
	if e != nil {
		t.Fatal(e)
	}
	im := previewContourSurface(g, &ContourSet{Surface: s, Params: p})
	_, _, _, a := im.At(6, 3).RGBA()
	if a != 0 {
		t.Fatal("cropped edge stretched to full source extent")
	}
	_, _, _, a = im.At(3, 3).RGBA()
	if a == 0 {
		t.Fatal("valid regional surface missing")
	}
}

func TestRegionalMeanAgainstReference(t *testing.T) {
	w, h, r := 7, 5, 2
	a := make([]float32, w*h)
	for i := range a {
		a[i] = float32(i * i % 43)
	}
	a[17] = float32(math.NaN())
	out, e := regionalMean(context.Background(), a, w, h, r)
	if e != nil {
		t.Fatal(e)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if !valid(a[i]) {
				if valid(out[i]) {
					t.Fatal("NoData filled")
				}
				continue
			}
			sum, n := 0.0, 0
			for yy := max(0, y-r); yy <= min(h-1, y+r); yy++ {
				for xx := max(0, x-r); xx <= min(w-1, x+r); xx++ {
					v := a[yy*w+xx]
					if valid(v) {
						sum += float64(v)
						n++
					}
				}
			}
			if math.Abs(float64(out[i])-sum/float64(n)) > 1e-5 {
				t.Fatal("running mean disagrees with window reference")
			}
		}
	}
}
func TestRegionalSmoothingAndFilter(t *testing.T) {
	g := &Grid{W: 51, H: 51, Z: make([]float32, 51*51)}
	g.Z[25*51+25] = 20
	p := defaultContourParams()
	p.MinArea = 0
	p.SmoothRadius = 0
	raw, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(raw.Lines) == 0 {
		t.Fatal("missing spike contours")
	}
	p.RegionalRadius = 5
	smooth, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if smooth.Surface.Z[25*51+25] >= 1 || g.Z[25*51+25] != 20 {
		t.Fatal("regional filter ineffective or source modified")
	}
	if len(smooth.Lines) >= len(raw.Lines) {
		t.Fatal("regional filter did not reduce local contours")
	}
	p.RegionalRadius = 0
	p.MinLength = 20
	filtered, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(filtered.Lines) != 0 {
		t.Fatal("short contour loops not suppressed")
	}
	plane := contourPlane()
	p.MinLength = 3
	long, e := generateContours(context.Background(), plane, plane.Z, p, 1, func(int, string) {})
	if e != nil || len(long.Lines) == 0 {
		t.Fatal("long regional contours discarded")
	}
}
