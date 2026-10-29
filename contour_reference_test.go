package main

import (
	"context"
	"errors"
	"math"
	"sort"
)

func referenceContours(ctx context.Context, g *Grid, z []float32, p ContourParams, revision int, progress func(int, string), masks ...[]float32) (*ContourSet, error) {
	if e := p.validate(); e != nil {
		return nil, e
	}
	if g == nil {
		return nil, errors.New("load an elevation raster first")
	}
	if p.MinArea > 0 && areaScaleM2(g) == 0 {
		return nil, errors.New("the square-metre area filter requires a projected raster with recognized metre/foot units; reproject the DTM, or set minimum enclosed area to 0 for unreferenced/angular data")
	}
	progress(5, "Preparing contour surface")
	surface, e := contourSurface(ctx, g, z, p, masks...)
	if e != nil {
		return nil, e
	}
	lo, hi := extent(surface.Z)
	if (hi-lo)/p.Interval > 4096 || math.Abs((lo-p.Base)/p.Interval) > 1e12 || math.Abs((hi-p.Base)/p.Interval) > 1e12 {
		return nil, errors.New("contour range exceeds 4096 levels or supported height range; increase interval")
	}
	nodes := []contourNode{}
	lookup := map[contourNodeKey]int{}
	segments := []contourSegment{}
	w, h := surface.W, surface.H
	factor := float64(p.AverageFactor)
	addNode := func(level int64, edge int, pt CellPoint) int {
		k := contourNodeKey{level, edge}
		if i, ok := lookup[k]; ok {
			return i
		}
		i := len(nodes)
		lookup[k] = i
		nodes = append(nodes, contourNode{point: pt})
		return i
	}
	for y := 0; y < h-1; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if y%32 == 0 {
			progress(15+y*65/max(1, h-1), "Tracing full-resolution contours")
		}
		for x := 0; x < w-1; x++ {
			v := [4]float64{float64(surface.Z[y*w+x]), float64(surface.Z[y*w+x+1]), float64(surface.Z[(y+1)*w+x+1]), float64(surface.Z[(y+1)*w+x])}
			ok := true
			for _, n := range v {
				if !validNumber(n) {
					ok = false
				}
			}
			if !ok {
				continue
			}
			vmin, vmax := v[0], v[0]
			for _, n := range v[1:] {
				vmin = math.Min(vmin, n)
				vmax = math.Max(vmax, n)
			}
			for level := int64(math.Ceil((vmin - p.Base) / p.Interval)); level <= int64(math.Floor((vmax-p.Base)/p.Interval)); level++ {
				elevation := p.Base + float64(level)*p.Interval
				edges := []int{}
				for edge := 0; edge < 4; edge++ {
					if (v[edge] >= elevation) != (v[(edge+1)%4] >= elevation) {
						edges = append(edges, edge)
					}
				}
				if len(edges) == 0 {
					continue
				}
				points := [4]CellPoint{{float64(x) + .5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + 1.5}, {float64(x) + .5, float64(y) + 1.5}}
				vertex := [4]int{y*w + x, y*w + x + 1, (y+1)*w + x + 1, (y+1)*w + x}
				edgeKeys := [4]int{y*w + x, w*h + y*w + x + 1, (y+1)*w + x, w*h + y*w + x}
				nodeFor := func(edge int) int {
					next := (edge + 1) % 4
					t := (elevation - v[edge]) / (v[next] - v[edge])
					a, b := points[edge], points[next]
					key := edgeKeys[edge]
					if t == 0 {
						key = 2*w*h + vertex[edge]
					} else if t == 1 {
						key = 2*w*h + vertex[next]
					}
					return addNode(level, key, CellPoint{(a.X + t*(b.X-a.X)) * factor, (a.Y + t*(b.Y-a.Y)) * factor})
				}
				connect := func(a, b int) {
					i, j := nodeFor(a), nodeFor(b)
					if i == j {
						return
					}
					s := len(segments)
					segments = append(segments, contourSegment{i, j})
					nodes[i].segments = append(nodes[i].segments, s)
					nodes[j].segments = append(nodes[j].segments, s)
				}
				if len(edges) == 2 {
					connect(edges[0], edges[1])
				} else if len(edges) == 4 {
					q := (v[0]-elevation)*(v[2]-elevation) - (v[1]-elevation)*(v[3]-elevation)
					if q >= 0 {
						connect(0, 1)
						connect(2, 3)
					} else {
						connect(3, 0)
						connect(1, 2)
					}
				}
				if len(segments) > 200000 {
					return nil, errors.New("more than 200,000 contour segments; increase interval, averaging or smoothing")
				}
			}
		}
	}
	progress(85, "Joining contour lines")
	used := make([]bool, len(segments))
	lines := []ContourLine{}
	// Sorting keys gives reproducible line order across runs.
	keys := make([]contourNodeKey, 0, len(lookup))
	for k := range lookup {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].level == keys[j].level {
			return keys[i].edge < keys[j].edge
		}
		return keys[i].level < keys[j].level
	})
	walk := func(start, seg int, level int64) {
		line := ContourLine{Elevation: p.Base + float64(level)*p.Interval, Index: level%int64(p.IndexEvery) == 0, Points: []CellPoint{nodes[start].point}}
		current := start
		for !used[seg] {
			used[seg] = true
			s := segments[seg]
			next := s.a
			if next == current {
				next = s.b
			}
			line.Points = append(line.Points, nodes[next].point)
			if next == start || len(nodes[next].segments) != 2 {
				break
			}
			a, b := nodes[next].segments[0], nodes[next].segments[1]
			seg = a
			if used[seg] {
				seg = b
			}
			current = next
		}
		if len(line.Points) >= 2 {
			lines = append(lines, line)
		}
	}
	for _, k := range keys {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		i := lookup[k]
		if len(nodes[i].segments) == 2 {
			continue
		}
		for _, seg := range nodes[i].segments {
			if !used[seg] {
				walk(i, seg, k.level)
			}
		}
	}
	for _, k := range keys {
		i := lookup[k]
		for _, seg := range nodes[i].segments {
			if !used[seg] {
				walk(i, seg, k.level)
			}
		}
	}
	kept := make([]ContourLine, 0, len(lines))
	keptSegments := 0
	removedSmallLoops := 0
	removedShortLines := 0
	for _, line := range lines {
		if area, closed := contourAreaM2(g, line); closed && areaScaleM2(g) > 0 {
			line.AreaM2 = &area
		}
		if p.MinArea > 0 {
			if area, closed := contourAreaM2(g, line); closed && area < p.MinArea-math.Max(1e-8, p.MinArea*1e-9) {
				removedSmallLoops++
				continue
			}
		}
		if p.MinLength > 0 && contourLength(g, line) < p.MinLength {
			removedShortLines++
			continue
		}
		kept = append(kept, line)
		keptSegments += len(line.Points) - 1
	}
	quality, e := contourQuality(ctx, g, z, surface, p, masks...)
	if e != nil {
		return nil, e
	}
	return &ContourSet{Params: p, Revision: revision, Lines: kept, Surface: surface, Segments: keptSegments, RemovedSmallLoops: removedSmallLoops, RemovedShortLines: removedShortLines, Quality: quality}, nil
}
