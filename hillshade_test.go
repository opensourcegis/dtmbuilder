package main

import (
	"image/color"
	"math"
	"testing"
)

func TestHillshadeLighting(t *testing.T) {
	g := &Grid{W: 5, H: 5, Z: make([]float32, 25), PixelSize: 1}
	for i := range g.Z {
		g.Z[i] = 100
	}
	basis := hillshadeBasis(g)
	if got := hillshadeAt(g, g.Z, 2, 2, basis); got != 180 {
		t.Fatalf("flat relief %d", got)
	}
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			g.Z[y*5+x] = float32(x)
		}
	}
	lit := hillshadeAt(g, g.Z, 2, 2, basis)
	for i := range g.Z {
		g.Z[i] = -g.Z[i]
	}
	dark := hillshadeAt(g, g.Z, 2, 2, basis)
	if lit <= 180 || dark >= 180 {
		t.Fatalf("lighting direction: %d %d", lit, dark)
	}
	g.Z[11] = float32(math.NaN())
	if got := hillshadeAt(g, g.Z, 2, 2, basis); got != dark {
		t.Fatal("NoData changed plane shading")
	}
}
func TestAllElevationPreviewsHillshade(t *testing.T) {
	g := demo()
	before := append([]float32(nil), g.Z...)
	for _, layer := range []string{"dsm", "cleaned", "dtm", "ndsm"} {
		im := preview(g, g.Z, layer)
		for y := 0; y < g.H; y++ {
			for x := 0; x < g.W; x++ {
				c := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
				if valid(g.Z[y*g.W+x]) {
					if c.R != c.G || c.G != c.B || c.A != 255 {
						t.Fatalf("%s is not grayscale", layer)
					}
				} else if c.A != 0 {
					t.Fatal("NoData must be transparent")
				}
			}
		}
	}
	for i, v := range before {
		if valid(v) && v != g.Z[i] {
			t.Fatal("preview modified elevations")
		}
	}
	g.PixelSize = 2
	b := hillshadeBasis(g)
	if b != [4]float64{2, 0, 0, -2} {
		t.Fatal("pixel size ignored")
	}
}
