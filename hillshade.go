package main

import "math"

// Pixel-axis basis in map coordinates: column=(a,c), row=(b,d).
func hillshadeBasis(g *Grid) [4]float64 {
	b := [4]float64{1, 0, 0, -1}
	if validNumber(g.PixelSize) && g.PixelSize > 0 {
		b = [4]float64{g.PixelSize, 0, 0, -g.PixelSize}
	}
	if g.Order == nil {
		return b
	}
	read := func(id uint16, n int) []float64 {
		t := g.Tags[id]
		if t.Type != 12 || len(t.Data) < n*8 {
			return nil
		}
		v := make([]float64, n)
		for i := range v {
			v[i] = math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
		}
		return v
	}
	if v := read(33550, 2); v != nil {
		b = [4]float64{v[0], 0, 0, -v[1]}
	}
	matrix := read(34264, 16)
	if matrix != nil {
		b = [4]float64{matrix[0], matrix[1], matrix[4], matrix[5]}
	}
	// Geographic coordinates use an approximate local metre scale, with elevation
	// assumed in metres. Projected rasters assume matching horizontal/vertical units.
	keys := g.Tags[34735].Data
	geographic := false
	for i := 8; i+8 <= len(keys); i += 8 {
		if g.Order.Uint16(keys[i:]) == 1024 && g.Order.Uint16(keys[i+2:]) == 0 && g.Order.Uint16(keys[i+6:]) == 2 {
			geographic = true
		}
	}
	if geographic {
		lat := 0.0
		if v := read(33922, 6); v != nil {
			lat = v[4] + (float64(g.H)/2-v[1])*b[3]
		}
		if matrix != nil {
			lat = matrix[7] + matrix[4]*float64(g.W)/2 + matrix[5]*float64(g.H)/2
		}
		mx := 111320 * math.Max(.01, math.Cos(lat*math.Pi/180))
		my := 110574.0
		b[0] *= mx
		b[1] *= mx
		b[2] *= my
		b[3] *= my
	}
	det := b[0]*b[3] - b[1]*b[2]
	if !validNumber(det) || math.Abs(det) < 1e-20 {
		return [4]float64{1, 0, 0, -1}
	}
	return b
}
func validNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Central differences with one-sided derivatives at edges and NoData boundaries.
// Lambertian relief, azimuth 315 degrees clockwise from north, altitude 45 degrees.
func hillshadeAt(g *Grid, z []float32, x, y int, b [4]float64) uint8 {
	v := float64(z[y*g.W+x])
	derivative := func(x1, y1, x2, y2 int) float64 {
		a, c := v, v
		left, right := 0, 0
		if x1 >= 0 && y1 >= 0 && x1 < g.W && y1 < g.H && valid(z[y1*g.W+x1]) {
			a = float64(z[y1*g.W+x1])
			left = 1
		}
		if x2 >= 0 && y2 >= 0 && x2 < g.W && y2 < g.H && valid(z[y2*g.W+x2]) {
			c = float64(z[y2*g.W+x2])
			right = 1
		}
		if left+right == 0 {
			return 0
		}
		return (c - a) / float64(left+right)
	}
	dc := derivative(x-1, y, x+1, y)
	dr := derivative(x, y-1, x, y+1)
	det := b[0]*b[3] - b[1]*b[2]
	gx := (b[3]*dc - b[2]*dr) / det
	gy := (-b[1]*dc + b[0]*dr) / det
	// Light vector = (-0.5 east, +0.5 north, sqrt(0.5) upward).
	illumination := (.5*gx - .5*gy + math.Sqrt(.5)) / math.Sqrt(1+gx*gx+gy*gy)
	return uint8(math.Round(255 * math.Max(0, math.Min(1, illumination))))
}
