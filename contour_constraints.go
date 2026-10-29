package main

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type ContourConstraint struct {
	ID        int        `json:"id"`
	X         float64    `json:"x"`
	Y         float64    `json:"y"`
	Elevation float64    `json:"elevation"`
	Radius    int        `json:"radius"`
	Cells     [4]int     `json:"cells"`
	Values    [4]float32 `json:"values"`
}

type ContourPinRequest struct {
	Line       int     `json:"line"`
	Vertex     int     `json:"vertex"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Radius     int     `json:"radius"`
	Revision   int     `json:"revision"`
	Generation uint64  `json:"generation"`
}

func bilinearTerrain(g *Grid, z []float32, p CellPoint) (value, gx, gy float64, cells [4]int, err error) {
	if p.X < .5 || p.Y < .5 || p.X > float64(g.W)-.5 || p.Y > float64(g.H)-.5 {
		err = errors.New("keep the contour handle inside the valid cell-centre extent")
		return
	}
	x, y := min(g.W-2, int(math.Floor(p.X-.5))), min(g.H-2, int(math.Floor(p.Y-.5)))
	cells = [4]int{y*g.W + x, y*g.W + x + 1, (y+1)*g.W + x, (y+1)*g.W + x + 1}
	v := [4]float64{}
	for k, i := range cells {
		if !valid(z[i]) {
			err = errors.New("a contour constraint cannot cross NoData; choose a valid location")
			return
		}
		v[k] = float64(z[i])
	}
	tx, ty := p.X-.5-float64(x), p.Y-.5-float64(y)
	value = (1-ty)*((1-tx)*v[0]+tx*v[1]) + ty*((1-tx)*v[2]+tx*v[3])
	gx = (1-ty)*(v[1]-v[0]) + ty*(v[3]-v[2])
	gy = (1-tx)*(v[2]-v[0]) + tx*(v[3]-v[1])
	return
}

// A drag changes the DTM, rather than creating an independent vector line that
// disagrees with it. Four protected samples define a local plane through the
// target elevation; surrounding changes feather to zero within the chosen radius.
func (s *studio) pinContour(q ContourPinRequest) error {
	c := s.contours
	if s.busy || s.contourBusy || c == nil || s.result == nil {
		return errors.New("wait for current contours before moving a handle")
	}
	if q.Revision != s.edits.Revision || q.Generation != c.Generation {
		return errors.New("contours changed; select a handle in the refreshed contour layer")
	}
	if c.Params.AverageFactor != 1 {
		return errors.New("set block average factor to 1 before constraining the DTM from a contour")
	}
	if q.Line < 0 || q.Line >= len(c.Lines) || q.Vertex < 0 || q.Vertex >= len(c.Lines[q.Line].Points) || q.Radius < 2 || q.Radius > 50 || !validNumber(q.X) || !validNumber(q.Y) {
		return errors.New("invalid contour handle or radius (2–50 source pixels)")
	}
	if len(s.constraints) >= 256 {
		return errors.New("the session supports 256 contour pins; remove an unused pin first")
	}
	line := c.Lines[q.Line]
	target := CellPoint{q.X, q.Y}
	_, gx, gy, _, e := bilinearTerrain(s.grid, c.Surface.Z, line.Points[q.Vertex])
	if e != nil {
		return e
	}
	if math.Hypot(gx, gy) < 1e-8 {
		return errors.New("this vertex has no defined local terrain slope; choose another contour handle")
	}
	_, _, _, anchors, e := bilinearTerrain(s.grid, s.result.DTM, target)
	if e != nil {
		return e
	}
	for _, pin := range s.constraints {
		if math.Hypot(pin.X-q.X, pin.Y-q.Y) < 1e-6 {
			return errors.New("this location already has a contour constraint")
		}
	}
	indices, _, e := selectBrushCells(s.grid, []CellPoint{target}, q.Radius)
	if e != nil {
		return e
	}
	anchorSet := map[int]bool{}
	for _, i := range anchors {
		anchorSet[i] = true
	}
	rec := EditRecord{Tool: "contour-pin", Points: []CellPoint{target}, Radius: q.Radius, Time: time.Now().UTC().Format(time.RFC3339)}
	elevation := line.Elevation
	rec.Elevation = &elevation
	before := append([]ContourConstraint{}, s.constraints...)
	pin := ContourConstraint{ID: s.nextConstraintID + 1, X: q.X, Y: q.Y, Elevation: elevation, Radius: q.Radius, Cells: anchors}
	for _, i := range indices {
		old := s.result.DTM[i]
		if !valid(old) {
			continue
		}
		x, y := float64(i%s.grid.W)+.5, float64(i/s.grid.W)+.5
		plane := elevation + gx*(x-q.X) + gy*(y-q.Y)
		distance2 := (x-q.X)*(x-q.X) + (y-q.Y)*(y-q.Y)
		weight := math.Pow(math.Max(0, 1-distance2/float64(q.Radius*q.Radius)), 2)
		if anchorSet[i] {
			weight = 1
		}
		value := float32(float64(old) + weight*(plane-float64(old)))
		if !valid(value) {
			return errors.New("constraint would overflow the DTM height range")
		}
		flags := int(s.result.Flags[i])
		if value != old && flags&(128|256|512|1024) != 0 {
			return errors.New("constraint conflicts with a protected water level or another contour pin; remove that pin or choose another location")
		}
		nextFlags := flags | 64
		if anchorSet[i] {
			nextFlags |= 256
		}
		nextFlags &^= 8
		if valid(s.result.Clean[i]) && s.result.Clean[i]-value > float32(s.params.GroundThreshold) {
			nextFlags |= 8
		}
		if value != old || nextFlags != flags {
			rec.Changes = append(rec.Changes, cellChange{i, old, value, s.result.Flags[i], float32(nextFlags)})
		}
		for k, j := range anchors {
			if i == j {
				pin.Values[k] = value
			}
		}
	}
	after := append(append([]ContourConstraint{}, before...), pin)
	rec.ConstraintsBefore = &before
	rec.ConstraintsAfter = &after
	rec.Cells = len(rec.Changes)
	s.nextConstraintID = pin.ID
	s.recordEdit(rec)
	s.message = fmt.Sprintf("Contour pinned at %.6g height units; DTM and contours refresh together", elevation)
	return nil
}

func (s *studio) removeContourPin(id, revision int) error {
	if s.busy || s.result == nil || revision != s.edits.Revision {
		return errors.New("terrain changed or extraction is running; refresh before removing a pin")
	}
	before := append([]ContourConstraint{}, s.constraints...)
	after := []ContourConstraint{}
	var removed *ContourConstraint
	for _, pin := range before {
		if pin.ID == id {
			p := pin
			removed = &p
		} else {
			after = append(after, pin)
		}
	}
	if removed == nil {
		return errors.New("constraint does not exist")
	}
	rec := EditRecord{Tool: "remove-contour-pin", Time: time.Now().UTC().Format(time.RFC3339), ConstraintsBefore: &before, ConstraintsAfter: &after}
	owned := map[int]bool{}
	for _, p := range after {
		for _, i := range p.Cells {
			owned[i] = true
		}
	}
	for _, i := range removed.Cells {
		if !owned[i] {
			old := s.result.DTM[i]
			flags := s.result.Flags[i]
			next := float32(int(flags) &^ 256)
			if next != flags {
				rec.Changes = append(rec.Changes, cellChange{i, old, old, flags, next})
			}
		}
	}
	rec.Cells = len(rec.Changes)
	s.recordEdit(rec)
	s.message = "Contour constraint removed; edited terrain heights retained"
	return nil
}
