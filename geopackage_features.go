package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

func featureGeometry(g *Grid, f TerrainFeature, srs int) ([]byte, [4]float64, error) {
	bounds := [4]float64{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
	var wkb bytes.Buffer
	wkb.WriteByte(1)
	put := func(v uint32) { binary.Write(&wkb, binary.LittleEndian, v) }
	writePoint := func(p CellPoint) error {
		x, y, ok := worldAt(g, p.X, p.Y)
		if !ok {
			x, y = p.X, p.Y
		}
		if !validNumber(x) || !validNumber(y) {
			return errors.New("invalid constraint coordinate")
		}
		bounds[0] = math.Min(bounds[0], x)
		bounds[1] = math.Max(bounds[1], x)
		bounds[2] = math.Min(bounds[2], y)
		bounds[3] = math.Max(bounds[3], y)
		binary.Write(&wkb, binary.LittleEndian, x)
		binary.Write(&wkb, binary.LittleEndian, y)
		return nil
	}
	if f.Kind == "breakline" {
		put(2)
		put(uint32(len(f.Points)))
		for _, p := range f.Points {
			if e := writePoint(p); e != nil {
				return nil, bounds, e
			}
		}
	} else {
		put(3)
		put(uint32(1 + len(f.Holes)))
		for _, ring := range append([][]CellPoint{f.Points}, f.Holes...) {
			put(uint32(len(ring) + 1))
			for _, p := range append(append([]CellPoint{}, ring...), ring[0]) {
				if e := writePoint(p); e != nil {
					return nil, bounds, e
				}
			}
		}
	}
	header := make([]byte, 40)
	copy(header, "GP")
	header[3] = 3
	binary.LittleEndian.PutUint32(header[4:], uint32(int32(srs)))
	for i, v := range bounds {
		binary.LittleEndian.PutUint64(header[8+i*8:], math.Float64bits(v))
	}
	return append(header, wkb.Bytes()...), bounds, nil
}
func exportConstraintLayers(ctx context.Context, tx *sql.Tx, g *Grid, c *ContourSet, srs int) error {
	for _, kind := range []string{"breakline", "waterbody"} {
		features := []TerrainFeature{}
		for _, f := range c.TerrainFeatures {
			if f.Kind == kind {
				features = append(features, f)
			}
		}
		if len(features) == 0 {
			continue
		}
		table, geometry := "terrain_breaklines", "LINESTRING"
		if kind == "waterbody" {
			table, geometry = "terrain_waterbodies", "POLYGON"
		}
		if _, e := tx.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (fid INTEGER PRIMARY KEY,geom %s NOT NULL,mode TEXT NOT NULL,start_height REAL,end_height REAL,elevation_unit TEXT NOT NULL,radius_pixels INTEGER NOT NULL)`, table, geometry)); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO gpkg_contents(table_name,data_type,identifier,description,srs_id) VALUES(?,'features',?,'Drawn terrain constraints; source-CRS coordinates, heights in declared source units',?)`, table, table, srs); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO gpkg_geometry_columns VALUES(?,'geom',?,?,0,0)`, table, geometry, srs); e != nil {
			return e
		}
		bounds := [4]float64{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
		for _, f := range features {
			b, extent, e := featureGeometry(g, f, srs)
			if e != nil {
				return e
			}
			var start, end any
			if f.Kind == "waterbody" || f.Mode != "preserve" {
				start, end = f.Elevation, f.EndElevation
			}
			if _, e = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES(?,?,?,?,?,?,?)`, table), f.ID, b, f.Mode, start, end, c.Params.ElevationUnit, f.Radius); e != nil {
				return e
			}
			bounds[0] = math.Min(bounds[0], extent[0])
			bounds[1] = math.Max(bounds[1], extent[1])
			bounds[2] = math.Min(bounds[2], extent[2])
			bounds[3] = math.Max(bounds[3], extent[3])
		}
		if _, e := tx.ExecContext(ctx, `UPDATE gpkg_contents SET min_x=?,min_y=?,max_x=?,max_y=? WHERE table_name=?`, bounds[0], bounds[2], bounds[1], bounds[3], table); e != nil {
			return e
		}
	}
	return nil
}
