package main

import (
	"context"
	"errors"
	"math"
)

// These are differences from the edited DTM, NOT accuracy against survey control.
// Each valid source cell is compared with its corresponding contour-grid block.
type ContourQuality struct {
	ComparedCells        int      `json:"comparedCells"`
	ChangedCells         int      `json:"changedCells"`
	MaskedSourceCells    int      `json:"maskedSourceCells"`
	CroppedSourceCells   int      `json:"croppedSourceCells"`
	MeanChange           float64  `json:"meanChange"`
	RMSEChange           float64  `json:"rmseChange"`
	MaxAbsChange         float64  `json:"maxAbsChange"`
	HydroSourceCells     int      `json:"hydroSourceCells"`
	PreservedHydroBlocks int      `json:"preservedHydroBlocks"`
	Warnings             []string `json:"warnings"`
}

func contourQuality(ctx context.Context, g *Grid, z []float32, surface *Grid, p ContourParams, masks ...[]float32) (ContourQuality, error) {
	q := ContourQuality{Warnings: []string{}, CroppedSourceCells: g.W*g.H - surface.W*surface.H*p.AverageFactor*p.AverageFactor}
	var flags []float32
	if len(masks) > 0 {
		flags = masks[0]
	}
	waterBlocks := map[int]bool{}
	sum, squares := 0.0, 0.0
	waterOnLevel := false
	for y := 0; y < g.H; y++ {
		if e := ctx.Err(); e != nil {
			return q, e
		}
		for x := 0; x < g.W; x++ {
			i := y*g.W + x
			water := len(flags) > 0 && valid(z[i]) && int(flags[i])&128 != 0
			if water {
				q.HydroSourceCells++
			}
			if x >= surface.W*p.AverageFactor || y >= surface.H*p.AverageFactor || !valid(z[i]) {
				continue
			}
			j := (y/p.AverageFactor)*surface.W + x/p.AverageFactor
			if !valid(surface.Z[j]) {
				q.MaskedSourceCells++
				continue
			}
			if water && p.PreserveHydro {
				waterBlocks[j] = true
				level := (float64(z[i]) - p.Base) / p.Interval
				if math.Abs(level-math.Round(level)) < 1e-6 {
					waterOnLevel = true
				}
			}
			d := float64(surface.Z[j]) - float64(z[i])
			q.ComparedCells++
			if d != 0 {
				q.ChangedCells++
			}
			sum += d
			squares += d * d
			q.MaxAbsChange = math.Max(q.MaxAbsChange, math.Abs(d))
		}
	}
	if q.ComparedCells == 0 {
		return q, errors.New("no valid contour surface remains; reduce averaging or provide valid elevations")
	}
	q.MeanChange = sum / float64(q.ComparedCells)
	q.RMSEChange = math.Sqrt(squares / float64(q.ComparedCells))
	q.PreservedHydroBlocks = len(waterBlocks)
	q.Warnings = append(q.Warnings, "Surface differences measure generalization relative to the edited DTM, not vertical accuracy against surveyed ground.")
	if p.ElevationUnit == "" || p.ElevationUnit == "native" {
		q.Warnings = append(q.Warnings, "Elevation units are unspecified. Interval and rounding step use source height units; horizontal CRS units do not establish vertical units.")
	}
	if p.ElevationQuantum >= p.Interval/2 {
		q.Warnings = append(q.Warnings, "Rounding is at least half the contour interval and may suppress real relief; reduce the rounding step.")
	}
	if p.ElevationQuantum > 0 && p.SmoothRadius == 0 {
		q.Warnings = append(q.Warnings, "Elevation rounding has no following focal mean. The report's anti-wormwood treatment rounds and then filters.")
	}
	if q.PreservedHydroBlocks == 0 {
		q.Warnings = append(q.Warnings, "No manually hydroflattened water cells are protected. This output does not establish conformity to lakes or rivers.")
	}
	if waterOnLevel {
		q.Warnings = append(q.Warnings, "A protected water elevation coincides with a contour level. The known water level is retained; shoreline routing requires additional hydrography treatment.")
	}
	if q.CroppedSourceCells > 0 {
		q.Warnings = append(q.Warnings, "Incomplete right/bottom averaging blocks were cropped; see croppedSourceCells.")
	}
	if p.MinArea > 0 || p.MinLength > 0 {
		q.Warnings = append(q.Warnings, "Size filtering can hide real hills and depressions. Compare with minimum area and minimum length set to zero.")
	}
	return q, nil
}

func contourReport(g *Grid, c *ContourSet) any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"reference":  "Tyler and Greenlee (2012), USGS SIR 2012–5167, pp. 18–21",
		"method":     "Separate contour surface: block average, optional elevation rounding, focal mean, optional regional means; marching squares. No TopoToRaster or PAEK implementation.",
		"parameters": c.Params, "revision": c.Revision,
		"sourceWidth": g.W, "sourceHeight": g.H, "surfaceWidth": c.Surface.W, "surfaceHeight": c.Surface.H,
		"horizontalUnit": horizontalUnit(g), "projectedEPSG": geoKey(g, 3072),
		"lines": len(c.Lines), "segments": c.Segments, "removedSmallLoops": c.RemovedSmallLoops, "removedShortLines": c.RemovedShortLines,
		"quality": c.Quality, "performance": c.Performance, "generation": c.Generation, "constraints": c.Constraints,
	}
}
