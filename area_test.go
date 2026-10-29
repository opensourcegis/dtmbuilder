package main

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func areaFixture() *Grid {
	g := &Grid{W: 61, H: 61, Z: make([]float32, 61*61), Order: binary.LittleEndian, Tags: map[uint16]tag{}, PixelSize: 1}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			small := math.Max(0, 6-2*math.Max(math.Abs(float64(x-15)), math.Abs(float64(y-15))))
			large := math.Max(0, 12-math.Max(math.Abs(float64(x-40)), math.Abs(float64(y-40))))
			g.Z[y*g.W+x] = float32(math.Max(small, large))
		}
	}
	set := func(id uint16, v []float64) {
		b := make([]byte, len(v)*8)
		for i, z := range v {
			g.Order.PutUint64(b[i*8:], math.Float64bits(z))
		}
		g.Tags[id] = tag{id, 12, uint32(len(v)), b}
	}
	set(33550, []float64{1, 1, 0})
	set(33922, []float64{0, 0, 0, 500000, 1000000, 0})
	b := make([]byte, 16)
	for i, v := range []uint16{1, 1, 0, 1, 3076, 0, 1, 9001} {
		g.Order.PutUint16(b[i*2:], v)
	}
	g.Tags[34735] = tag{34735, 3, 8, b}
	return g
}
func TestSmallContourAreaFilter(t *testing.T) {
	g := areaFixture()
	p := ContourParams{Interval: 100, Base: 1, IndexEvery: 5, AverageFactor: 1}
	raw, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil || len(raw.Lines) != 2 {
		t.Fatalf("expected two closed hills: %v %v", raw, e)
	}
	p.MinArea = 50
	c, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Lines) != 1 || c.RemovedSmallLoops != 1 || c.Lines[0].AreaM2 == nil || *c.Lines[0].AreaM2 < 50 {
		t.Fatal("50 m² area filter failed")
	}
	smallArea := math.Inf(1)
	for _, l := range raw.Lines {
		a, _ := contourAreaM2(g, l)
		smallArea = math.Min(smallArea, a)
	}
	p.MinArea = smallArea
	exact, e := generateContours(context.Background(), g, g.Z, p, 1, func(int, string) {})
	if e != nil || len(exact.Lines) != 2 {
		t.Fatal("area exactly at threshold must remain")
	}
	if g.Z[15*61+15] != 6 {
		t.Fatal("area filter changed DTM")
	}
	p.MinArea = 50
	plane := contourPlane()
	plane.Order = g.Order
	plane.Tags = g.Tags
	open, e := generateContours(context.Background(), plane, plane.Z, p, 1, func(int, string) {})
	if e != nil || len(open.Lines) == 0 || open.RemovedSmallLoops != 0 {
		t.Fatal("open contours must remain despite small footprint")
	}
}
func TestAreaUnitsAndNormalDefaults(t *testing.T) {
	g := areaFixture()
	line := ContourLine{Points: []CellPoint{{0, 0}, {5, 0}, {5, 10}, {0, 10}, {0, 0}}}
	a, closed := contourAreaM2(g, line)
	if !closed || math.Abs(a-50) > 1e-8 {
		t.Fatal("square metres incorrect")
	}
	keys := g.Tags[34735]
	g.Order.PutUint16(keys.Data[14:], 9002)
	scale := g.Tags[33550]
	for i := 0; i < 2; i++ {
		g.Order.PutUint64(scale.Data[i*8:], math.Float64bits(1/.3048))
	}
	a, closed = contourAreaM2(g, line)
	if !closed || math.Abs(a-50) > 1e-6 {
		t.Fatal("square feet not converted to square metres")
	}
	p := defaultContourParams()
	if p.AverageFactor != 1 || p.SmoothRadius != 0 || p.RegionalRadius != 0 || p.MinLength != 0 || p.MinArea != 50 {
		t.Fatal("defaults must be normal unsmoothed contours with 50 m² filter")
	}
	if _, e := generateContours(context.Background(), contourPlane(), contourPlane().Z, p, 1, func(int, string) {}); e == nil {
		t.Fatal("unreferenced pixels must not be treated as metres")
	}
}
