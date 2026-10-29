package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	sqliteDriver "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/rtree"
)

//go:embed data/epsg-wkt.json.gz
var epsgCatalog []byte
var epsgOnce sync.Once

type epsgDefinition struct {
	WKT  string `json:"wkt"`
	Unit string `json:"unit"`
}

var epsgWKT map[string]epsgDefinition
var epsgError error

func lookupWKT(code int) (string, error) {
	epsgOnce.Do(func() {
		r, e := gzip.NewReader(bytes.NewReader(epsgCatalog))
		if e != nil {
			epsgError = e
			return
		}
		defer r.Close()
		epsgError = json.NewDecoder(r).Decode(&epsgWKT)
	})
	if epsgError != nil {
		return "", epsgError
	}
	wkt, ok := epsgWKT[strconv.Itoa(code)]
	if !ok {
		return "", fmt.Errorf("EPSG:%d is not in the offline CRS catalog", code)
	}
	return wkt.WKT, nil
}

func sourceSRS(g *Grid) (int, string, string, error) {
	if _, _, ok := worldAt(g, 0, 0); !ok {
		return -1, "undefined", "Source pixel coordinates; no georeference", nil
	}
	code := int(geoKey(g, 3072))
	if geoKey(g, 1024) == 2 {
		code = int(geoKey(g, 2048))
	}
	if code == 0 || code == 32767 {
		id := -1
		if horizontalUnit(g) == "degrees" {
			id = 0
		}
		return id, "undefined", "Unknown source CRS; coordinates retained without assigning a false EPSG code", nil
	}
	wkt, e := lookupWKT(code)
	return code, wkt, "Source raster CRS; XY coordinates retained without reprojection", e
}

func contourGeometry(g *Grid, line ContourLine, srs int) ([]byte, [4]float64, error) {
	bounds := [4]float64{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
	if len(line.Points) < 2 {
		return nil, bounds, errors.New("a contour needs at least two vertices")
	}
	b := make([]byte, 40+9+16*len(line.Points))
	copy(b, "GP")
	b[2] = 0
	b[3] = 3
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(srs)))
	b[40] = 1
	binary.LittleEndian.PutUint32(b[41:], 2)
	binary.LittleEndian.PutUint32(b[45:], uint32(len(line.Points)))
	for i, p := range line.Points {
		x, y, ok := worldAt(g, p.X, p.Y)
		if !ok {
			x, y = p.X, p.Y
		}
		if !validNumber(x) || !validNumber(y) {
			return nil, bounds, errors.New("invalid contour coordinate")
		}
		bounds[0] = math.Min(bounds[0], x)
		bounds[1] = math.Max(bounds[1], x)
		bounds[2] = math.Min(bounds[2], y)
		bounds[3] = math.Max(bounds[3], y)
		binary.LittleEndian.PutUint64(b[49+i*16:], math.Float64bits(x))
		binary.LittleEndian.PutUint64(b[57+i*16:], math.Float64bits(y))
	}
	for i, v := range bounds {
		binary.LittleEndian.PutUint64(b[8+i*8:], math.Float64bits(v))
	}
	return b, bounds, nil
}

// Export one consistent contour revision. SQLite, its spatial index and the CRS
// catalog are linked into the Go executable; no GDAL, Python or network is used.
func contourGPKG(ctx context.Context, g *Grid, c *ContourSet) ([]byte, error) {
	if g == nil || c == nil {
		return nil, errors.New("generate current contours first")
	}
	srs, wkt, description, e := sourceSRS(g)
	if e != nil {
		return nil, e
	}
	temp, e := os.MkdirTemp("", "terrain-gpkg-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(temp)
	path := filepath.Join(temp, "contours.gpkg")
	db, e := openGeoPackageDB(path)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ddl := []string{
		`PRAGMA application_id=1196444487`, `PRAGMA user_version=10400`, `PRAGMA foreign_keys=ON`,
		`CREATE TABLE gpkg_spatial_ref_sys (srs_name TEXT NOT NULL,srs_id INTEGER PRIMARY KEY,organization TEXT NOT NULL,organization_coordsys_id INTEGER NOT NULL,definition TEXT NOT NULL,description TEXT)`,
		`CREATE TABLE gpkg_contents (table_name TEXT NOT NULL PRIMARY KEY,data_type TEXT NOT NULL,identifier TEXT UNIQUE,description TEXT DEFAULT '',last_change DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE,srs_id INTEGER REFERENCES gpkg_spatial_ref_sys(srs_id))`,
		`CREATE TABLE gpkg_geometry_columns (table_name TEXT NOT NULL,column_name TEXT NOT NULL,geometry_type_name TEXT NOT NULL,srs_id INTEGER NOT NULL,z TINYINT NOT NULL,m TINYINT NOT NULL,PRIMARY KEY(table_name,column_name),UNIQUE(table_name),FOREIGN KEY(table_name) REFERENCES gpkg_contents(table_name),FOREIGN KEY(srs_id) REFERENCES gpkg_spatial_ref_sys(srs_id))`,
		`CREATE TABLE gpkg_extensions (table_name TEXT,column_name TEXT,extension_name TEXT NOT NULL,definition TEXT NOT NULL,scope TEXT NOT NULL,UNIQUE(table_name,column_name,extension_name))`,
		`CREATE TABLE contours (fid INTEGER PRIMARY KEY,geom LINESTRING NOT NULL,elevation REAL NOT NULL,index_contour BOOLEAN NOT NULL,contour_interval REAL NOT NULL,elevation_unit TEXT NOT NULL,revision INTEGER NOT NULL,area_m2 REAL,depression BOOLEAN)`,
		`CREATE VIRTUAL TABLE rtree_contours_geom USING rtree(id,minx,maxx,miny,maxy)`,
		`CREATE TABLE terrain_report (id INTEGER PRIMARY KEY,report_json TEXT NOT NULL)`,
	}
	for _, q := range ddl {
		if _, e = db.ExecContext(ctx, q); e != nil {
			return nil, e
		}
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	addSRS := func(id int, name, org string, orgID int, definition, desc string) error {
		_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gpkg_spatial_ref_sys VALUES(?,?,?,?,?,?)`, name, id, org, orgID, definition, desc)
		return e
	}
	wgs, e := lookupWKT(4326)
	if e != nil {
		return nil, e
	}
	for _, r := range []struct {
		id        int
		name, org string
		orgID     int
		def       string
	}{{-1, "Undefined Cartesian", "NONE", -1, "undefined"}, {0, "Undefined geographic", "NONE", 0, "undefined"}, {4326, "WGS 84", "EPSG", 4326, wgs}} {
		if e = addSRS(r.id, r.name, r.org, r.orgID, r.def, ""); e != nil {
			return nil, e
		}
	}
	if srs > 0 {
		if e = addSRS(srs, fmt.Sprintf("EPSG:%d", srs), "EPSG", srs, wkt, description); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO gpkg_contents(table_name,data_type,identifier,description,srs_id) VALUES('contours','features','contours',?,?)`, description, srs); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO gpkg_geometry_columns VALUES('contours','geom','LINESTRING',?,0,0)`, srs); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO gpkg_contents(table_name,data_type,identifier,description) VALUES('terrain_report','attributes','terrain_report','Contour parameters, quality, constraints and source revision')`); e != nil {
		return nil, e
	}
	stmt, e := tx.PrepareContext(ctx, `INSERT INTO contours(fid,geom,elevation,index_contour,contour_interval,elevation_unit,revision,area_m2) VALUES(?,?,?,?,?,?,?,?)`)
	if e != nil {
		return nil, e
	}
	defer stmt.Close()
	index, e := tx.PrepareContext(ctx, `INSERT INTO rtree_contours_geom VALUES(?,?,?,?,?)`)
	if e != nil {
		return nil, e
	}
	defer index.Close()
	extent := [4]float64{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
	for i, line := range c.Lines {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		geometry, b, e := contourGeometry(g, line, srs)
		if e != nil {
			return nil, e
		}
		var area any
		if line.AreaM2 != nil {
			area = *line.AreaM2
		}
		if _, e = stmt.ExecContext(ctx, i+1, geometry, line.Elevation, line.Index, c.Params.Interval, c.Params.ElevationUnit, c.Revision, area); e != nil {
			return nil, e
		}
		if _, e = index.ExecContext(ctx, i+1, b[0], b[1], b[2], b[3]); e != nil {
			return nil, e
		}
		extent[0] = math.Min(extent[0], b[0])
		extent[1] = math.Max(extent[1], b[1])
		extent[2] = math.Min(extent[2], b[2])
		extent[3] = math.Max(extent[3], b[3])
	}
	if len(c.Lines) > 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE gpkg_contents SET min_x=?,min_y=?,max_x=?,max_y=? WHERE table_name='contours'`, extent[0], extent[2], extent[1], extent[3]); e != nil {
			return nil, e
		}
	}
	report, e := json.Marshal(contourReport(g, c))
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO terrain_report VALUES(1,?)`, string(report)); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO gpkg_extensions VALUES('contours','geom','gpkg_rtree_index','http://www.geopackage.org/spec/#extension_rtree','write-only')`); e != nil {
		return nil, e
	}
	for _, q := range gpkgIndexTriggers() {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			return nil, e
		}
	}
	if e = exportConstraintLayers(ctx, tx, g, c, srs); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	if e = db.Close(); e != nil {
		return nil, e
	}
	return os.ReadFile(path)
}

func gpkgIndexTriggers() []string {
	insert := `INSERT OR REPLACE INTO rtree_contours_geom VALUES(NEW.fid,ST_MinX(NEW.geom),ST_MaxX(NEW.geom),ST_MinY(NEW.geom),ST_MaxY(NEW.geom));`
	return []string{
		`CREATE TRIGGER rtree_contours_geom_insert AFTER INSERT ON contours WHEN NEW.geom NOT NULL AND NOT ST_IsEmpty(NEW.geom) BEGIN ` + insert + ` END`,
		`CREATE TRIGGER rtree_contours_geom_update2 AFTER UPDATE OF geom ON contours WHEN OLD.fid=NEW.fid AND (NEW.geom ISNULL OR ST_IsEmpty(NEW.geom)) BEGIN DELETE FROM rtree_contours_geom WHERE id=OLD.fid; END`,
		`CREATE TRIGGER rtree_contours_geom_update4 AFTER UPDATE ON contours WHEN OLD.fid!=NEW.fid AND (NEW.geom ISNULL OR ST_IsEmpty(NEW.geom)) BEGIN DELETE FROM rtree_contours_geom WHERE id IN (OLD.fid,NEW.fid); END`,
		`CREATE TRIGGER rtree_contours_geom_update5 AFTER UPDATE ON contours WHEN OLD.fid!=NEW.fid AND NEW.geom NOTNULL AND NOT ST_IsEmpty(NEW.geom) BEGIN DELETE FROM rtree_contours_geom WHERE id=OLD.fid; ` + insert + ` END`,
		`CREATE TRIGGER rtree_contours_geom_update6 AFTER UPDATE OF geom ON contours WHEN OLD.fid=NEW.fid AND NEW.geom NOTNULL AND NOT ST_IsEmpty(NEW.geom) AND OLD.geom NOTNULL AND NOT ST_IsEmpty(OLD.geom) BEGIN UPDATE rtree_contours_geom SET minx=ST_MinX(NEW.geom),maxx=ST_MaxX(NEW.geom),miny=ST_MinY(NEW.geom),maxy=ST_MaxY(NEW.geom) WHERE id=NEW.fid; END`,
		`CREATE TRIGGER rtree_contours_geom_update7 AFTER UPDATE OF geom ON contours WHEN OLD.fid=NEW.fid AND NEW.geom NOTNULL AND NOT ST_IsEmpty(NEW.geom) AND (OLD.geom ISNULL OR ST_IsEmpty(OLD.geom)) BEGIN ` + insert + ` END`,
		`CREATE TRIGGER rtree_contours_geom_delete AFTER DELETE ON contours WHEN OLD.geom NOT NULL BEGIN DELETE FROM rtree_contours_geom WHERE id=OLD.fid; END`,
	}
}

func openGeoPackageDB(path string) (*sql.DB, error) { return sqliteDriver.Open(path, rtree.Register) }

func epsgHorizontalUnit(code uint16) string {
	if code == 0 || code == 32767 {
		return ""
	}
	if _, e := lookupWKT(int(code)); e != nil {
		return ""
	}
	return epsgWKT[strconv.Itoa(int(code))].Unit
}
