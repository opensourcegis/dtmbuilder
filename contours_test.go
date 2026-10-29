package main

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func contourPlane() *Grid {
	g := &Grid{W: 6, H: 5, Z: make([]float32, 30), Tags: map[uint16]tag{}}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Z[y*g.W+x] = float32(x)
		}
	}
	return g
}
func TestContourPlaneJoining(t *testing.T) {
	g := contourPlane()
	p := ContourParams{Interval: 1, Base: .5, IndexEvery: 2, AverageFactor: 1}
	c, e := generateContours(context.Background(), g, g.Z, p, 7, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Lines) != 5 || c.Revision != 7 {
		t.Fatalf("expected five joined contour lines, got %d", len(c.Lines))
	}
	for _, line := range c.Lines {
		if len(line.Points) != 5 {
			t.Fatalf("segments not joined: %v", line)
		}
		for _, pt := range line.Points {
			if math.Abs(pt.X-(line.Elevation+.5)) > 1e-9 {
				t.Fatal("contour interpolation incorrect")
			}
		}
	}
	if g.Z[1] != 1 {
		t.Fatal("DTM modified")
	}
}
func TestContourNoDataAndClosedLoop(t *testing.T) {
	g := contourPlane()
	g.Z[2*g.W+3] = float32(math.NaN())
	p := ContourParams{Interval: 100, Base: 2.5, IndexEvery: 5, AverageFactor: 1}
	c, e := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Lines) != 2 {
		t.Fatalf("NoData should split the contour into two lines: %d", len(c.Lines))
	}
	for _, line := range c.Lines {
		for _, pt := range line.Points {
			if pt.Y > 1.5 && pt.Y < 3.5 {
				t.Fatal("contour crossed NoData")
			}
		}
	}
	g = &Grid{W: 9, H: 9, Z: make([]float32, 81)}
	for y := 0; y < 9; y++ {
		for x := 0; x < 9; x++ {
			g.Z[y*9+x] = float32(10 - math.Hypot(float64(x-4), float64(y-4)))
		}
	}
	p = ContourParams{Interval: 100, Base: 8.3, IndexEvery: 5, AverageFactor: 1}
	c, e = generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Lines) != 1 {
		t.Fatalf("expected one closed hill contour, got %d", len(c.Lines))
	}
	pts := c.Lines[0].Points
	if pts[0] != pts[len(pts)-1] {
		t.Fatal("closed loop not joined")
	}
}
func TestContourAveragingGeoreference(t *testing.T) {
	g := &Grid{W: 5, H: 5, Z: make([]float32, 25), Order: binary.LittleEndian, Tags: map[uint16]tag{}, PixelSize: .1}
	for i := range g.Z {
		g.Z[i] = float32(i)
	}
	makeTag := func(id uint16, v []float64) {
		b := make([]byte, len(v)*8)
		for i, n := range v {
			g.Order.PutUint64(b[i*8:], math.Float64bits(n))
		}
		g.Tags[id] = tag{id, 12, uint32(len(v)), b}
	}
	makeTag(33550, []float64{.1, .2, 0})
	makeTag(33922, []float64{2, 1, 0, 500, 1000, 0})
	p := ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 2}
	s, e := contourSurface(context.Background(), g, g.Z, p)
	if e != nil {
		t.Fatal(e)
	}
	if s.W != 2 || s.H != 2 || s.Z[0] != 3 {
		t.Fatal("block average incorrect")
	}
	x, y, ok := worldAt(s, .5, .5)
	xx, yy, _ := worldAt(g, 1, 1)
	if !ok || math.Abs(x-xx) > 1e-9 || math.Abs(y-yy) > 1e-9 {
		t.Fatal("averaging shifted georeferencing")
	}
	if math.Float64frombits(g.Order.Uint64(g.Tags[33550].Data)) != .1 {
		t.Fatal("source georeference mutated")
	}
	b, e := encodeTIFF(s, s.Z)
	if e != nil {
		t.Fatal(e)
	}
	round, e := decodeTIFF(b)
	if e != nil || round.PixelSize != .2 {
		t.Fatal("averaged grid export lost resolution")
	}
	g.Z[0] = float32(math.NaN())
	s, e = contourSurface(context.Background(), g, g.Z, p)
	if e != nil || valid(s.Z[0]) {
		t.Fatal("averaging must preserve blocks containing NoData")
	}
}
func TestContourValidationAndCancellation(t *testing.T) {
	g := contourPlane()
	p := defaultContourParams()
	p.MinArea = 0
	p.Interval = 0
	if _, e := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {}); e == nil {
		t.Fatal("zero interval accepted")
	}
	p = defaultContourParams()
	p.MinArea = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := generateContours(ctx, g, g.Z, p, 0, func(int, string) {}); e == nil {
		t.Fatal("cancellation ignored")
	}
	p.SmoothRadius = 0
	g.Z = []float32{1, -1, -1, 1}
	g.W = 2
	g.H = 2
	p.Interval = 100
	p.Base = 0
	c, e := generateContours(context.Background(), g, g.Z, p, 0, func(int, string) {})
	if e != nil || len(c.Lines) != 2 {
		t.Fatal("saddle cell not resolved deterministically")
	}
}
