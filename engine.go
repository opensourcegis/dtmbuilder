package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
)

type Params struct {
	Radius          int     `json:"radius"`
	Low             float64 `json:"low"`
	High            float64 `json:"high"`
	Spike           float64 `json:"spike"`
	MAD             float64 `json:"mad"`
	Support         float64 `json:"support"`
	ObjectRadius    int     `json:"objectRadius"`
	GroundThreshold float64 `json:"groundThreshold"`
	Slope           float64 `json:"slope"`
	Smooth          int     `json:"smooth"`
	Fill            int     `json:"fill"`
	FillOutput      bool    `json:"fillOutput"`
}

func defaults() Params {
	return Params{Radius: 2, Low: 2, High: 5, Spike: 1, MAD: 4, Support: .25, ObjectRadius: 24, GroundThreshold: .5, Slope: .05, Smooth: 2, Fill: 8}
}
func (p Params) validate() error {
	for _, v := range []float64{p.Low, p.High, p.Spike, p.MAD, p.Support, p.GroundThreshold, p.Slope} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return errors.New("thresholds must be finite and nonnegative")
		}
	}
	if p.Radius < 1 || p.Radius > 8 || p.ObjectRadius < 0 || p.ObjectRadius > 512 || p.Smooth < 0 || p.Smooth > 50 || p.Fill < 0 || p.Fill > 100 || p.Support > 0.5 || p.MAD > 30 {
		return errors.New("parameters outside supported ranges")
	}
	return nil
}

type Result struct {
	Clean, DTM, NDSM []float32
	Flags            []float32
	Counts           map[string]int
}

func valid(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func median(v []float64) float64 {
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}
func process(ctx context.Context, g *Grid, p Params, progress func(int, string)) (*Result, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	n := len(g.Z)
	clean := append([]float32(nil), g.Z...)
	flags := make([]float32, n)
	counts := map[string]int{"low": 0, "high": 0, "spike": 0, "nonground": 0, "nodata": 0, "filled": 0, "filledOutput": 0, "remainingNoData": 0}
	neighbors := make([]float64, 0, (2*p.Radius+1)*(2*p.Radius+1))
	deviations := make([]float64, 0, cap(neighbors))
	for y := 0; y < g.H; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if y%32 == 0 {
			progress(y*25/g.H, "Robust outlier filtering")
		}
		for x := 0; x < g.W; x++ {
			i := y*g.W + x
			z := g.Z[i]
			if !valid(z) {
				flags[i] = 16
				counts["nodata"]++
				continue
			}
			neighbors = neighbors[:0]
			for yy := max(0, y-p.Radius); yy <= min(g.H-1, y+p.Radius); yy++ {
				for xx := max(0, x-p.Radius); xx <= min(g.W-1, x+p.Radius); xx++ {
					if xx == x && yy == y {
						continue
					}
					v := g.Z[yy*g.W+xx]
					if valid(v) {
						neighbors = append(neighbors, float64(v))
					}
				}
			}
			if len(neighbors) < 5 {
				continue
			}
			m := median(neighbors)
			deviations = deviations[:0]
			supported := 0
			for _, v := range neighbors {
				deviations = append(deviations, math.Abs(v-m))
				if math.Abs(v-float64(z)) <= math.Max(0.01, p.Spike) {
					supported++
				}
			}
			robust := p.MAD * 1.4826 * median(deviations)
			d := float64(z) - m
			bit := 0
			key := ""
			if p.Low > 0 && d < -math.Max(p.Low, robust) {
				bit = 1
				key = "low"
			} else if p.High > 0 && d > math.Max(p.High, robust) {
				bit = 2
				key = "high"
			} else if p.Spike > 0 && math.Abs(d) > math.Max(p.Spike, robust) && float64(supported)/float64(len(neighbors)) <= p.Support {
				bit = 4
				key = "spike"
			}
			if bit > 0 {
				clean[i] = float32(m)
				flags[i] = float32(bit)
				counts[key]++
			}
		}
	}
	// Fill only small reachable gaps in working data. Unresolved cells stay NaN.
	for step := 0; step < p.Fill; step++ {
		progress(25+step*4/max(1, p.Fill), "Interpolating NoData gaps")
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next := append([]float32(nil), clean...)
		changed := 0
		for i, z := range clean {
			if valid(z) {
				continue
			}
			x, y := i%g.W, i/g.W
			sum := 0.0
			k := 0
			for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				xx, yy := x+d[0], y+d[1]
				if xx >= 0 && xx < g.W && yy >= 0 && yy < g.H {
					v := clean[yy*g.W+xx]
					if valid(v) {
						sum += float64(v)
						k++
					}
				}
			}
			if k > 0 {
				next[i] = float32(sum / float64(k))
				changed++
			}
		}
		clean = next
		counts["filled"] += changed
		if changed == 0 {
			break
		}
	}
	terrain := append([]float32(nil), clean...)
	radii := []int{}
	for r := 1; r < p.ObjectRadius; r *= 2 {
		radii = append(radii, r)
	}
	if p.ObjectRadius > 0 {
		radii = append(radii, p.ObjectRadius)
	}
	for step, r := range radii {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress(30+step*45/max(1, len(radii)), fmt.Sprintf("Terrain opening · radius %d px", r))
		eroded, err := extreme(ctx, terrain, g.W, g.H, r, true)
		if err != nil {
			return nil, err
		}
		opened, err := extreme(ctx, eroded, g.W, g.H, r, false)
		if err != nil {
			return nil, err
		}
		threshold := p.GroundThreshold + p.Slope*float64(r)
		for i, v := range terrain {
			if valid(v) && valid(opened[i]) && float64(v-opened[i]) > threshold {
				terrain[i] = opened[i]
			}
		}
	}
	for step := 0; step < p.Smooth; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress(78+step*14/max(1, p.Smooth), "Constrained surface smoothing")
		next := append([]float32(nil), terrain...)
		for y := 0; y < g.H; y++ {
			for x := 0; x < g.W; x++ {
				i := y*g.W + x
				if !valid(terrain[i]) {
					continue
				}
				sum := 0.0
				k := 0
				for yy := max(0, y-1); yy <= min(g.H-1, y+1); yy++ {
					for xx := max(0, x-1); xx <= min(g.W-1, x+1); xx++ {
						v := terrain[yy*g.W+xx]
						if valid(v) {
							sum += float64(v)
							k++
						}
					}
				}
				next[i] = min(clean[i], float32(sum/float64(k)))
			}
		}
		terrain = next
	}
	ndsm := make([]float32, n)
	for i := range terrain {
		if !valid(g.Z[i]) {
			if !p.FillOutput || !valid(clean[i]) || !valid(terrain[i]) {
				clean[i] = float32(math.NaN())
				terrain[i] = float32(math.NaN())
				ndsm[i] = float32(math.NaN())
				counts["remainingNoData"]++
				continue
			}
			flags[i] = float32(int(flags[i]) | 32)
			counts["filledOutput"]++
		}
		ndsm[i] = max(0, clean[i]-terrain[i])
		if ndsm[i] > float32(p.GroundThreshold) {
			flags[i] = float32(int(flags[i]) | 8)
			counts["nonground"]++
		}
	}
	return &Result{clean, terrain, ndsm, flags, counts}, nil
}

// Separable sliding min/max, O(width*height), independent of window size.
func extreme(ctx context.Context, src []float32, w, h, r int, lower bool) ([]float32, error) {
	tmp := make([]float32, len(src))
	dst := make([]float32, len(src))
	line := make([]int, max(w, h))
	for pass := 0; pass < 2; pass++ {
		a, b, outer, inner := src, tmp, h, w
		if pass == 1 {
			a, b, outer, inner = tmp, dst, w, h
		}
		for o := 0; o < outer; o++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			head, tail, next := 0, 0, 0
			idx := func(j int) int {
				if pass == 0 {
					return o*w + j
				}
				return j*w + o
			}
			for j := 0; j < inner; j++ {
				for next < inner && next <= j+r {
					v := a[idx(next)]
					if valid(v) {
						for tail > head {
							last := a[idx(line[tail-1])]
							if (lower && last < v) || (!lower && last > v) {
								break
							}
							tail--
						}
						line[tail] = next
						tail++
					}
					next++
				}
				for head < tail && line[head] < j-r {
					head++
				}
				if head == tail {
					b[idx(j)] = float32(math.NaN())
				} else {
					b[idx(j)] = a[idx(line[head])]
				}
			}
		}
	}
	return dst, nil
}
