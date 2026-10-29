package main

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func TestTerrainAndOutliers(t *testing.T) {
	g := demo()
	r, err := process(context.Background(), g, defaults(), func(int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"low", "high", "spike", "nonground"} {
		if r.Counts[k] == 0 {
			t.Errorf("no %s detected", k)
		}
	}
	if r.NDSM[65*g.W+60] < 8 {
		t.Error("building was not removed")
	}
	for i, v := range g.Z {
		if !valid(v) {
			if valid(r.DTM[i]) || valid(r.Clean[i]) {
				t.Fatal("NoData lost")
			}
			continue
		}
		if r.DTM[i] > r.Clean[i]+.0001 {
			t.Fatal("DTM exceeds cleaned surface")
		}
	}
}
func TestPreserveFlatAndCancel(t *testing.T) {
	g := &Grid{W: 20, H: 20, Z: make([]float32, 400)}
	for i := range g.Z {
		g.Z[i] = 125
	}
	r, e := process(context.Background(), g, defaults(), func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range r.DTM {
		if v != 125 {
			t.Fatal("flat terrain changed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = process(ctx, g, defaults(), func(int, string) {}); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestRoundTrip(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		g := demo()
		g.Order = bo
		g.Z[0] = float32(math.NaN())
		b, e := encodeTIFF(g, g.Z)
		if e != nil {
			t.Fatal(e)
		}
		h, e := decodeTIFF(b)
		if e != nil {
			t.Fatal(e)
		}
		if h.W != g.W || h.H != g.H {
			t.Fatal("dimensions changed")
		}
		for i, v := range g.Z {
			if valid(v) && v != h.Z[i] {
				t.Fatal("height changed")
			}
			if !valid(v) && valid(h.Z[i]) {
				t.Fatal("NoData changed")
			}
		}
	}
}
func TestMalformedTIFF(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("II*\x00\xff\xff\xff\xff"), []byte("MM\x00+\x00\x00\x00\x08")} {
		if _, e := decodeTIFF(b); e == nil {
			t.Fatal("malformed input accepted")
		}
	}
}

func TestNoDataOutputFilling(t *testing.T) {
	g := &Grid{W: 9, H: 9, Z: make([]float32, 81)}
	for i := range g.Z {
		g.Z[i] = 25
	}
	for y := 3; y <= 5; y++ {
		for x := 3; x <= 5; x++ {
			g.Z[y*9+x] = float32(math.NaN())
		}
	}
	p := defaults()
	p.ObjectRadius = 0
	p.Smooth = 0
	p.Fill = 1
	p.FillOutput = true
	r, e := process(context.Background(), g, p, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if r.Counts["filledOutput"] != 8 || r.Counts["remainingNoData"] != 1 {
		t.Fatalf("bounded filling: %v", r.Counts)
	}
	if valid(r.DTM[4*9+4]) {
		t.Fatal("filled beyond configured distance")
	}
	p.Fill = 3
	r, e = process(context.Background(), g, p, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if r.Counts["filledOutput"] != 9 || r.Counts["remainingNoData"] != 0 {
		t.Fatalf("fill count: %v", r.Counts)
	}
	for y := 3; y <= 5; y++ {
		for x := 3; x <= 5; x++ {
			i := y*9 + x
			if r.DTM[i] != 25 || r.Clean[i] != 25 || r.NDSM[i] != 0 || int(r.Flags[i]) != 48 {
				t.Fatalf("bad interpolated output at %d", i)
			}
			if valid(g.Z[i]) {
				t.Fatal("source modified")
			}
		}
	}
	b, e := encodeTIFF(g, r.DTM)
	if e != nil {
		t.Fatal(e)
	}
	roundTrip, e := decodeTIFF(b)
	if e != nil {
		t.Fatal(e)
	}
	if roundTrip.Z[40] != 25 {
		t.Fatal("export lost interpolated value")
	}
	p.FillOutput = false
	r, e = process(context.Background(), g, p, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if valid(r.DTM[40]) || r.Counts["filledOutput"] != 0 {
		t.Fatal("preserve mode changed original NoData")
	}
	p.FillOutput = true
	p.Fill = 0
	r, e = process(context.Background(), g, p, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	if r.Counts["remainingNoData"] != 9 {
		t.Fatal("zero fill distance did not disable filling")
	}
}
