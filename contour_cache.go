package main

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

const contourTileSide = 128
const maxContourSegments = 2000000

type cellRect struct {
	X0, Y0, X1, Y1 int
	Valid          bool
}

func (r cellRect) union(q cellRect) cellRect {
	if !r.Valid {
		return q
	}
	if !q.Valid {
		return r
	}
	return cellRect{min(r.X0, q.X0), min(r.Y0, q.Y0), max(r.X1, q.X1), max(r.Y1, q.Y1), true}
}
func (r cellRect) expand(n, w, h int) cellRect {
	return cellRect{max(0, r.X0-n), max(0, r.Y0-n), min(w-1, r.X1+n), min(h-1, r.Y1+n), r.Valid}
}

type cachedSegment struct {
	A, B contourNodeKey
	P, Q CellPoint
}
type contourCache struct {
	Source  *Grid
	Params  ContourParams
	Surface *Grid
	Tiles   [][]cachedSegment
}
type ContourPerformance struct {
	Mode                string  `json:"mode"`
	TilesUpdated        int     `json:"tilesUpdated"`
	TilesReused         int     `json:"tilesReused"`
	SurfaceCellsUpdated int     `json:"surfaceCellsUpdated"`
	TotalMilliseconds   float64 `json:"totalMilliseconds"`
}

func generateContours(ctx context.Context, g *Grid, z []float32, p ContourParams, revision int, progress func(int, string), masks ...[]float32) (*ContourSet, error) {
	c, _, e := generateContoursCached(ctx, g, z, p, revision, progress, nil, cellRect{}, masks...)
	return c, e
}

func generateContoursCached(ctx context.Context, g *Grid, z []float32, p ContourParams, revision int, progress func(int, string), prior *contourCache, dirty cellRect, masks ...[]float32) (*ContourSet, *contourCache, error) {
	started := time.Now()
	if e := p.validate(); e != nil {
		return nil, nil, e
	}
	if g == nil || len(z) != g.W*g.H {
		return nil, nil, errors.New("load a matching elevation raster first")
	}
	if len(masks) > 0 && len(masks[0]) > 0 && len(masks[0]) != len(z) {
		return nil, nil, errors.New("constraint mask must match the elevation raster")
	}
	if p.MinArea > 0 && areaScaleM2(g) == 0 {
		return nil, nil, errors.New("the square-metre area filter requires a projected raster with recognized metre/foot units; reproject the DTM, or set minimum enclosed area to 0 for unreferenced/angular data")
	}
	progress(5, "Preparing contour surface")
	var surface *Grid
	var changed cellRect
	var e error
	incremental := prior != nil && prior.Source == g && prior.Params == p && dirty.Valid
	if incremental {
		if len(masks) > 0 {
			if err := validateConstraintFilters(p, masks[0]); err != nil {
				return nil, nil, err
			}
		}
		surface, changed, e = updateContourSurface(ctx, g, z, p, prior.Surface, dirty, masks...)
	} else {
		surface, e = contourSurface(ctx, g, z, p, masks...)
		if e == nil {
			changed = cellRect{0, 0, surface.W - 1, surface.H - 1, true}
		}
	}
	if e != nil {
		return nil, nil, e
	}
	lo, hi := extent(surface.Z)
	if (hi-lo)/p.Interval > 4096 || math.Abs((lo-p.Base)/p.Interval) > 1e12 || math.Abs((hi-p.Base)/p.Interval) > 1e12 {
		return nil, nil, errors.New("contour range exceeds 4096 levels or supported height range; increase interval")
	}
	w, h := surface.W, surface.H
	cols, rows := (w+contourTileSide-1)/contourTileSide, (h+contourTileSide-1)/contourTileSide
	cache := &contourCache{Source: g, Params: p, Surface: surface, Tiles: make([][]cachedSegment, cols*rows)}
	if incremental {
		copy(cache.Tiles, prior.Tiles)
	}
	selected := []int{}
	for id := range cache.Tiles {
		tx, ty := id%cols, id/cols
		x0, y0 := tx*contourTileSide, ty*contourTileSide
		if !incremental || (changed.Valid && x0 <= changed.X1 && y0 <= changed.Y1 && x0+contourTileSide > changed.X0-1 && y0+contourTileSide > changed.Y0-1) {
			selected = append(selected, id)
		}
	}
	type tileResult struct {
		id       int
		segments []cachedSegment
		err      error
	}
	jobs := make(chan int)
	results := make(chan tileResult)
	workers := min(4, runtime.GOMAXPROCS(0), max(1, len(selected)))
	var wg sync.WaitGroup
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				segments, e := traceContourTile(workerCtx, surface, p, id%cols*contourTileSide, id/cols*contourTileSide)
				select {
				case results <- tileResult{id, segments, e}:
				case <-workerCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, id := range selected {
			select {
			case jobs <- id:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	done := 0
	segmentBudget := 0
	for _, tile := range cache.Tiles {
		segmentBudget += len(tile)
	}
	var traceError error
	for result := range results {
		if result.err != nil {
			if traceError == nil {
				traceError = result.err
			}
			cancel()
			continue
		}
		segmentBudget += len(result.segments) - len(cache.Tiles[result.id])
		if segmentBudget > maxContourSegments {
			traceError = errors.New("more than 2 million contour segments; increase interval or averaging")
			cancel()
			continue
		}
		cache.Tiles[result.id] = result.segments
		done++
		progress(15+done*60/max(1, len(selected)), "Refreshing contour tiles")
	}
	if traceError != nil {
		return nil, nil, traceError
	}
	if e = ctx.Err(); e != nil {
		return nil, nil, e
	}
	nodes := []contourNode{}
	lookup := map[contourNodeKey]int{}
	segments := []contourSegment{}
	nodeFor := func(key contourNodeKey, point CellPoint) int {
		if i, ok := lookup[key]; ok {
			return i
		}
		i := len(nodes)
		lookup[key] = i
		nodes = append(nodes, contourNode{point: point})
		return i
	}
	for _, tile := range cache.Tiles {
		if e = ctx.Err(); e != nil {
			return nil, nil, e
		}
		for _, s := range tile {
			if len(segments) >= maxContourSegments {
				return nil, nil, errors.New("more than 2 million contour segments; increase interval or averaging")
			}
			i, j := nodeFor(s.A, s.P), nodeFor(s.B, s.Q)
			k := len(segments)
			segments = append(segments, contourSegment{i, j})
			nodes[i].segments = append(nodes[i].segments, k)
			nodes[j].segments = append(nodes[j].segments, k)
		}
	}
	c, e := joinContours(ctx, g, z, p, revision, surface, nodes, lookup, segments, progress, masks...)
	if e != nil {
		return nil, nil, e
	}
	mode := "full"
	if incremental {
		mode = "incremental"
	}
	cells := 0
	if changed.Valid {
		cells = (changed.X1 - changed.X0 + 1) * (changed.Y1 - changed.Y0 + 1)
	}
	c.Performance = ContourPerformance{mode, len(selected), len(cache.Tiles) - len(selected), cells, float64(time.Since(started).Microseconds()) / 1000}
	return c, cache, nil
}

func updateContourSurface(ctx context.Context, g *Grid, z []float32, p ContourParams, old *Grid, dirty cellRect, masks ...[]float32) (*Grid, cellRect, error) {
	f := p.AverageFactor
	w, h := old.W, old.H
	core := cellRect{dirty.X0 / f, dirty.Y0 / f, min(w-1, dirty.X1/f), min(h-1, dirty.Y1/f), true}
	if core.X0 >= w || core.Y0 >= h {
		copyGrid := *old
		return &copyGrid, cellRect{}, nil
	}
	radius := p.SmoothRadius + p.EdgePasses + 3*p.RegionalRadius
	changed := core.expand(radius, w, h)
	input := changed.expand(radius, w, h)
	// The surface constructor needs at least 2x2 blocks even for a one-cell edit.
	if input.X0 == input.X1 {
		input.X0 = max(0, input.X0-1)
		input.X1 = min(w-1, input.X1+1)
	}
	if input.Y0 == input.Y1 {
		input.Y0 = max(0, input.Y0-1)
		input.Y1 = min(h-1, input.Y1+1)
	}
	cw, ch := (input.X1-input.X0+1)*f, (input.Y1-input.Y0+1)*f
	crop := &Grid{W: cw, H: ch, Z: make([]float32, cw*ch)}
	var flags []float32
	if len(masks) > 0 && len(masks[0]) > 0 {
		flags = make([]float32, len(crop.Z))
	}
	for y := 0; y < ch; y++ {
		if e := ctx.Err(); e != nil {
			return nil, cellRect{}, e
		}
		start := (input.Y0*f+y)*g.W + input.X0*f
		copy(crop.Z[y*cw:(y+1)*cw], z[start:start+cw])
		if flags != nil {
			copy(flags[y*cw:(y+1)*cw], masks[0][start:start+cw])
		}
	}
	patch, e := contourSurface(ctx, crop, crop.Z, p, flags)
	if e != nil {
		return nil, cellRect{}, e
	}
	out := *old
	out.Z = append([]float32(nil), old.Z...)
	for y := changed.Y0; y <= changed.Y1; y++ {
		start := (y-input.Y0)*patch.W + changed.X0 - input.X0
		copy(out.Z[y*w+changed.X0:y*w+changed.X1+1], patch.Z[start:start+changed.X1-changed.X0+1])
	}
	return &out, changed, nil
}

func traceContourTile(ctx context.Context, surface *Grid, p ContourParams, x0, y0 int) ([]cachedSegment, error) {
	w, h := surface.W, surface.H
	factor := float64(p.AverageFactor)
	segments := []cachedSegment{}
	for y := y0; y < min(h-1, y0+contourTileSide); y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		for x := x0; x < min(w-1, x0+contourTileSide); x++ {
			v := [4]float64{float64(surface.Z[y*w+x]), float64(surface.Z[y*w+x+1]), float64(surface.Z[(y+1)*w+x+1]), float64(surface.Z[(y+1)*w+x])}
			if !validNumber(v[0]) || !validNumber(v[1]) || !validNumber(v[2]) || !validNumber(v[3]) {
				continue
			}
			lo, hi := math.Min(math.Min(v[0], v[1]), math.Min(v[2], v[3])), math.Max(math.Max(v[0], v[1]), math.Max(v[2], v[3]))
			for level := int64(math.Ceil((lo - p.Base) / p.Interval)); level <= int64(math.Floor((hi-p.Base)/p.Interval)); level++ {
				elevation := p.Base + float64(level)*p.Interval
				var edges [4]int
				n := 0
				for edge := 0; edge < 4; edge++ {
					if (v[edge] >= elevation) != (v[(edge+1)%4] >= elevation) {
						edges[n] = edge
						n++
					}
				}
				if n == 0 {
					continue
				}
				points := [4]CellPoint{{float64(x) + .5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + .5}, {float64(x) + 1.5, float64(y) + 1.5}, {float64(x) + .5, float64(y) + 1.5}}
				vertex := [4]int{y*w + x, y*w + x + 1, (y+1)*w + x + 1, (y+1)*w + x}
				keys := [4]int{y*w + x, w*h + y*w + x + 1, (y+1)*w + x, w*h + y*w + x}
				endpoint := func(edge int) (contourNodeKey, CellPoint) {
					next := (edge + 1) % 4
					t := (elevation - v[edge]) / (v[next] - v[edge])
					a, b := points[edge], points[next]
					key := keys[edge]
					if t == 0 {
						key = 2*w*h + vertex[edge]
					} else if t == 1 {
						key = 2*w*h + vertex[next]
					}
					return contourNodeKey{level, key}, CellPoint{(a.X + t*(b.X-a.X)) * factor, (a.Y + t*(b.Y-a.Y)) * factor}
				}
				connect := func(a, b int) {
					ka, pa := endpoint(a)
					kb, pb := endpoint(b)
					if ka != kb {
						segments = append(segments, cachedSegment{ka, kb, pa, pb})
					}
				}
				if n == 2 {
					connect(edges[0], edges[1])
				} else if n == 4 {
					q := (v[0]-elevation)*(v[2]-elevation) - (v[1]-elevation)*(v[3]-elevation)
					if q >= 0 {
						connect(0, 1)
						connect(2, 3)
					} else {
						connect(3, 0)
						connect(1, 2)
					}
				}
				if len(segments) > maxContourSegments {
					return nil, errors.New("contour tile exceeds the segment budget; increase interval or averaging")
				}
			}
		}
	}
	return segments, nil
}

func joinContours(ctx context.Context, g *Grid, z []float32, p ContourParams, revision int, surface *Grid, nodes []contourNode, lookup map[contourNodeKey]int, segments []contourSegment, progress func(int, string), masks ...[]float32) (*ContourSet, error) {
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
		line.ID = len(kept)
		kept = append(kept, line)
		keptSegments += len(line.Points) - 1
	}
	quality, e := contourQuality(ctx, g, z, surface, p, masks...)
	if e != nil {
		return nil, e
	}
	return &ContourSet{Params: p, Revision: revision, Lines: kept, Surface: surface, Segments: keptSegments, RemovedSmallLoops: removedSmallLoops, RemovedShortLines: removedShortLines, Quality: quality}, nil
}
