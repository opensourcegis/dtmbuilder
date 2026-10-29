package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strings"
)

type ContourParams struct {
	Interval         float64 `json:"interval"`
	Base             float64 `json:"base"`
	IndexEvery       int     `json:"indexEvery"`
	AverageFactor    int     `json:"averageFactor"`
	SmoothRadius     int     `json:"smoothRadius"`
	RegionalRadius   int     `json:"regionalRadius"`
	MinLength        float64 `json:"minLength"`
	MinArea          float64 `json:"minArea"`
	ElevationQuantum float64 `json:"elevationQuantum"`
	ElevationUnit    string  `json:"elevationUnit"`
	PreserveHydro    bool    `json:"preserveHydro"`
}

func defaultContourParams() ContourParams {
	return ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 1, MinArea: 50, PreserveHydro: true}
}
func (p ContourParams) validate() error {
	if !validNumber(p.ElevationQuantum) || p.ElevationQuantum < 0 || p.ElevationQuantum > 1e8 {
		return errors.New("elevation rounding step must be finite and between 0 and 100,000,000 height units; 0 disables rounding")
	}
	switch p.ElevationUnit {
	case "", "native", "m", "ft", "US ft":
	default:
		return errors.New("elevation units must be unspecified, m, ft, or US ft; declaring units does not convert elevations")
	}
	if !validNumber(p.Interval) || p.Interval < .0001 || !validNumber(p.Base) || math.Abs(p.Base) > 1e8 || p.IndexEvery < 1 || p.IndexEvery > 100 || p.AverageFactor < 1 || p.AverageFactor > 100 || p.SmoothRadius < 0 || p.SmoothRadius > 5 || p.RegionalRadius < 0 || p.RegionalRadius > 100 || !validNumber(p.MinLength) || p.MinLength < 0 || !validNumber(p.MinArea) || p.MinArea < 0 {
		return errors.New("invalid contour settings: interval ≥0.0001, index 1–100, averaging 1–100, smoothing radius 0–5")
	}
	return nil
}

type ContourLine struct {
	ID        int         `json:"id"`
	Elevation float64     `json:"elevation"`
	Index     bool        `json:"index"`
	Points    []CellPoint `json:"points"`
	AreaM2    *float64    `json:"areaM2,omitempty"`
}
type ContourSet struct {
	Constraints       []ContourConstraint `json:"constraints"`
	Generation        uint64              `json:"generation"`
	Performance       ContourPerformance  `json:"performance"`
	Params            ContourParams       `json:"parameters"`
	Revision          int                 `json:"revision"`
	Lines             []ContourLine       `json:"lines"`
	Surface           *Grid               `json:"-"`
	Segments          int                 `json:"segments"`
	RemovedSmallLoops int                 `json:"removedSmallLoops"`
	RemovedShortLines int                 `json:"removedShortLines"`
	Quality           ContourQuality      `json:"quality"`
}
type contourNodeKey struct {
	level int64
	edge  int
}
type contourNode struct {
	point    CellPoint
	segments []int
}
type contourSegment struct{ a, b int }

func contourSurface(ctx context.Context, g *Grid, z []float32, p ContourParams, masks ...[]float32) (*Grid, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if g == nil || g.W < 2 || g.H < 2 || len(z) != g.W*g.H {
		return nil, errors.New("contouring requires a matching elevation grid of at least 2×2 cells")
	}
	var flags []float32
	if len(masks) > 0 {
		flags = masks[0]
	}
	if len(flags) != 0 && len(flags) != len(z) {
		return nil, errors.New("water flags do not match the elevation grid")
	}
	f := p.AverageFactor
	w, h := g.W/f, g.H/f
	if w < 2 || h < 2 {
		return nil, errors.New("averaging leaves fewer than 2×2 cells; reduce averaging factor")
	}
	a := make([]float32, w*h)
	nan := float32(math.NaN())
	// Constraints apply to the separate contour grid, never the edited DTM.
	water := map[int]float32{}
	for y := 0; y < h; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		for x := 0; x < w; x++ {
			sum := 0.0
			ok := true
			waterLevel, hasWater := float32(0), false
			for yy := y * f; yy < (y+1)*f; yy++ {
				for xx := x * f; xx < (x+1)*f; xx++ {
					j := yy*g.W + xx
					v := z[j]
					if !valid(v) {
						ok = false
					}
					sum += float64(v)
					if len(flags) > 0 && int(flags[j])&256 != 0 && valid(v) {
						if f != 1 {
							return nil, errors.New("pinned contour elevations require block average factor 1; remove pins before averaging")
						}
						water[y*w+x] = v
					}

					if p.PreserveHydro && len(flags) > 0 && int(flags[j])&128 != 0 && valid(v) {
						if hasWater && waterLevel != v {
							return nil, errors.New("an averaging block contains different hydroflattened levels; reduce the block average factor")
						}
						waterLevel, hasWater = v, true
					}
				}
			}
			if ok {
				a[y*w+x] = float32(sum / float64(f*f))
				if hasWater {
					a[y*w+x] = waterLevel
					water[y*w+x] = waterLevel
				}
			} else {
				a[y*w+x] = nan
			}
		}
	}
	// USGS SIR 2012–5167, p. 21: round elevations, then filter. The step
	// uses declared SOURCE height units; horizontal CRS units are not evidence
	// of vertical units. float64 division avoids overflowing a float32 scale.
	if p.ElevationQuantum > 0 {
		for i, v := range a {
			if i%4096 == 0 {
				if e := ctx.Err(); e != nil {
					return nil, e
				}
			}
			if !valid(v) {
				continue
			}
			if _, fixed := water[i]; fixed {
				continue
			}
			ratio := float64(v) / p.ElevationQuantum
			if !validNumber(ratio) || math.Abs(ratio) > 1e15 {
				return nil, errors.New("rounding step is too small for the elevation range")
			}
			rounded := float32(math.Round(ratio) * p.ElevationQuantum)
			if !valid(rounded) {
				return nil, errors.New("rounded elevation exceeds float32 output range")
			}
			a[i] = rounded
		}
	}
	if p.SmoothRadius > 0 {
		var err error
		a, err = regionalMean(ctx, a, w, h, p.SmoothRadius)
		if err != nil {
			return nil, err
		}
		for i, v := range water {
			a[i] = v
		}
	}

	if p.RegionalRadius > 0 {
		for pass := 0; pass < 3; pass++ {
			var e error
			a, e = regionalMean(ctx, a, w, h, p.RegionalRadius)
			if e != nil {
				return nil, e
			}
			for i, v := range water {
				a[i] = v
			}
		}
	}
	tags := map[uint16]tag{}
	for k, v := range g.Tags {
		v.Data = append([]byte(nil), v.Data...)
		tags[k] = v
	}
	// Preserve the upper-left grid origin, including non-zero raster tiepoints.
	if g.Order != nil && f != 1 {
		if t, ok := tags[33550]; ok && len(t.Data) >= 16 {
			for i := 0; i < 2; i++ {
				v := math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
				g.Order.PutUint64(t.Data[i*8:], math.Float64bits(v*float64(f)))
			}
			tags[33550] = t
		}
		if t, ok := tags[33922]; ok {
			for i := 0; i+48 <= len(t.Data); i += 48 {
				for j := 0; j < 2; j++ {
					v := math.Float64frombits(g.Order.Uint64(t.Data[i+j*8:]))
					if geoKey(g, 1025) == 2 {
						v = (v+.5)/float64(f) - .5
					} else {
						v /= float64(f)
					}
					g.Order.PutUint64(t.Data[i+j*8:], math.Float64bits(v))
				}
			}
			tags[33922] = t
		}
		if t, ok := tags[34264]; ok && len(t.Data) >= 128 {
			if geoKey(g, 1025) == 2 {
				for _, row := range []int{0, 4, 8} {
					a := math.Float64frombits(g.Order.Uint64(t.Data[row*8:]))
					b := math.Float64frombits(g.Order.Uint64(t.Data[(row+1)*8:]))
					d := math.Float64frombits(g.Order.Uint64(t.Data[(row+3)*8:]))
					g.Order.PutUint64(t.Data[(row+3)*8:], math.Float64bits(d+.5*float64(f-1)*(a+b)))
				}
			}
			for _, i := range []int{0, 1, 4, 5, 8, 9} {
				v := math.Float64frombits(g.Order.Uint64(t.Data[i*8:]))
				g.Order.PutUint64(t.Data[i*8:], math.Float64bits(v*float64(f)))
			}
			tags[34264] = t
		}
	}
	return &Grid{W: w, H: h, Z: a, Tags: tags, Order: g.Order, PixelSize: g.PixelSize * float64(f)}, nil
}

func areaScaleM2(g *Grid) float64 {
	metres := 0.0
	switch horizontalUnit(g) {
	case "m":
		metres = 1
	case "ft":
		metres = .3048
	case "US ft":
		metres = 1200.0 / 3937
	}
	if metres == 0 {
		return 0
	}
	x, y, ok := worldAt(g, 0, 0)
	if !ok {
		return 0
	}
	xx, xy, _ := worldAt(g, 1, 0)
	yx, yy, _ := worldAt(g, 0, 1)
	return math.Abs((xx-x)*(yy-y)-(yx-x)*(xy-y)) * metres * metres
}
func contourAreaM2(g *Grid, line ContourLine) (float64, bool) {
	p := line.Points
	if len(p) < 4 || p[0] != p[len(p)-1] {
		return 0, false
	}
	origin := p[0]
	twice := 0.0
	for i := 1; i < len(p); i++ {
		a, b := p[i-1], p[i]
		twice += (a.X-origin.X)*(b.Y-origin.Y) - (b.X-origin.X)*(a.Y-origin.Y)
	}
	return math.Abs(twice) * .5 * areaScaleM2(g), true
}

// Separable running sums keep broad smoothing O(width*height), independent of radius.
// Three normalized box passes approximate Gaussian low-pass smoothing. Missing
// cells remain missing in every pass; they are never used as zero elevations.
func regionalMean(ctx context.Context, a []float32, w, h, r int) ([]float32, error) {
	sums := make([]float64, len(a))
	counts := make([]uint16, len(a))
	out := make([]float32, len(a))
	nan := float32(math.NaN())
	for y := 0; y < h; y++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		sum := 0.0
		n := 0
		next := 0
		for x := 0; x < w; x++ {
			for next < w && next <= x+r {
				v := a[y*w+next]
				if valid(v) {
					sum += float64(v)
					n++
				}
				next++
			}
			if old := x - r - 1; old >= 0 {
				v := a[y*w+old]
				if valid(v) {
					sum -= float64(v)
					n--
				}
			}
			sums[y*w+x] = sum
			counts[y*w+x] = uint16(n)
		}
	}
	for x := 0; x < w; x++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		sum := 0.0
		n := 0
		next := 0
		for y := 0; y < h; y++ {
			for next < h && next <= y+r {
				i := next*w + x
				sum += sums[i]
				n += int(counts[i])
				next++
			}
			if old := y - r - 1; old >= 0 {
				i := old*w + x
				sum -= sums[i]
				n -= int(counts[i])
			}
			i := y*w + x
			if valid(a[i]) && n > 0 {
				out[i] = float32(sum / float64(n))
			} else {
				out[i] = nan
			}
		}
	}
	return out, nil
}
func contourLength(g *Grid, line ContourLine) float64 {
	a, b, c, d := 1.0, 0.0, 0.0, 1.0
	if x, y, ok := worldAt(g, 0, 0); ok {
		xx, xy, _ := worldAt(g, 1, 0)
		yx, yy, _ := worldAt(g, 0, 1)
		a, c, b, d = xx-x, xy-y, yx-x, yy-y
	}
	length := 0.0
	for i := 1; i < len(line.Points); i++ {
		p, q := line.Points[i-1], line.Points[i]
		dx, dy := q.X-p.X, q.Y-p.Y
		length += math.Hypot(a*dx+b*dy, c*dx+d*dy)
	}
	return length
}

func geoKey(g *Grid, id uint16) uint16 {
	if g.Order == nil {
		return 0
	}
	data := g.Tags[34735].Data
	for i := 8; i+8 <= len(data); i += 8 {
		if g.Order.Uint16(data[i:]) == id && g.Order.Uint16(data[i+2:]) == 0 && g.Order.Uint16(data[i+4:]) == 1 {
			return g.Order.Uint16(data[i+6:])
		}
	}
	return 0
}
func horizontalUnit(g *Grid) string {
	if _, _, ok := worldAt(g, 0, 0); !ok {
		return "px"
	}
	if geoKey(g, 1024) == 2 {
		return "degrees"
	}
	switch geoKey(g, 3076) {
	case 9001:
		return "m"
	case 9002:
		return "ft"
	case 9003:
		return "US ft"
	}
	code := geoKey(g, 3072)
	if code >= 32601 && code <= 32660 || code >= 32701 && code <= 32760 {
		return "m"
	}
	if unit := epsgHorizontalUnit(code); unit != "" {
		return unit
	}
	return "map units"
}

func contourCSV(g *Grid, c *ContourSet) ([]byte, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"elevation", "index_contour", "wkt", "coordinate_system"})
	for _, line := range c.Lines {
		parts := make([]string, len(line.Points))
		system := "source CRS (see source raster)"
		for i, p := range line.Points {
			x, y, ok := worldAt(g, p.X, p.Y)
			if !ok {
				x, y = p.X, p.Y
				system = "source pixel coordinates (no georeference)"
			}
			parts[i] = fmt.Sprintf("%.9f %.9f", x, y)
		}
		if e := w.Write([]string{fmt.Sprintf("%.8g", line.Elevation), fmt.Sprint(line.Index), "LINESTRING (" + strings.Join(parts, ", ") + ")", system}); e != nil {
			return nil, e
		}
	}
	w.Flush()
	return []byte(b.String()), w.Error()
}
