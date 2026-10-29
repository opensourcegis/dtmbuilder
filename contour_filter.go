package main

import "context"

// A four-neighbour, explicit Perona–Malik diffusion adaptation for elevations.
// c(d)=1/(1+(d/K)^2); dt=.2 stays below the .25 stability bound. NoData
// neighbours contribute nothing. Fixed samples are Dirichlet boundaries, so
// a continuous rasterized breakline separates the two sides during diffusion.
// This is not the normal-vector algorithm of Sun/Lindsay or PAEK smoothing.
func edgePreservingSurface(ctx context.Context, input []float32, w, h, passes int, contrast float64, fixed map[int]float32) ([]float32, error) {
	a := append([]float32(nil), input...)
	b := make([]float32, len(a))
	for range passes {
		for y := range h {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			for x := range w {
				i := y*w + x
				v := a[i]
				if _, protected := fixed[i]; protected || !valid(v) {
					b[i] = v
					continue
				}
				sum := 0.0
				add := func(j int) {
					if valid(a[j]) {
						d := float64(a[j]) - float64(v)
						r := d / contrast
						sum += d / (1 + r*r)
					}
				}
				if x > 0 {
					add(i - 1)
				}
				if x+1 < w {
					add(i + 1)
				}
				if y > 0 {
					add(i - w)
				}
				if y+1 < h {
					add(i + w)
				}
				b[i] = float32(float64(v) + .2*sum)
			}
		}
		a, b = b, a
	}
	return a, nil
}
