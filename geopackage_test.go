package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestGeoPackageSchemaGeometryAndSpatialIndex(t *testing.T) {
	g := areaFixture()
	c, e := generateContours(context.Background(), g, g.Z, ContourParams{Interval: 2, IndexEvery: 5, AverageFactor: 1}, 4, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	b, e := contourGPKG(context.Background(), g, c)
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:16]) != "SQLite format 3\x00" || binary.BigEndian.Uint32(b[68:]) != 0x47504b47 || binary.BigEndian.Uint32(b[60:]) != 10400 {
		t.Fatal("invalid GeoPackage header")
	}
	path := filepath.Join(t.TempDir(), "contours.gpkg")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	db, e := openGeoPackageDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var check string
	if e = db.QueryRow("PRAGMA integrity_check").Scan(&check); e != nil || check != "ok" {
		t.Fatalf("SQLite integrity: %s %v", check, e)
	}
	var count, indexed, triggers int
	db.QueryRow("SELECT count(*) FROM contours").Scan(&count)
	db.QueryRow("SELECT count(*) FROM rtree_contours_geom").Scan(&indexed)
	db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='trigger'").Scan(&triggers)
	if count != len(c.Lines) || indexed != count || triggers != 7 {
		t.Fatal("features, spatial index, or maintenance triggers missing")
	}
	var geom []byte
	var revision, srs int
	var elevation float64
	if e = db.QueryRow("SELECT geom,elevation,revision FROM contours WHERE fid=1").Scan(&geom, &elevation, &revision); e != nil {
		t.Fatal(e)
	}
	if string(geom[:2]) != "GP" || geom[3] != 3 || binary.LittleEndian.Uint32(geom[41:]) != 2 || revision != 4 {
		t.Fatal("invalid GeoPackage geometry/attributes")
	}
	db.QueryRow("SELECT srs_id FROM gpkg_geometry_columns").Scan(&srs)
	if int(int32(binary.LittleEndian.Uint32(geom[4:]))) != srs {
		t.Fatal("geometry SRS does not match metadata")
	}
	x, y, _ := worldAt(g, c.Lines[0].Points[0].X, c.Lines[0].Points[0].Y)
	if math.Float64frombits(binary.LittleEndian.Uint64(geom[49:])) != x || math.Float64frombits(binary.LittleEndian.Uint64(geom[57:])) != y || elevation != c.Lines[0].Elevation {
		t.Fatal("contour coordinates/elevation changed during export")
	}
	var report string
	db.QueryRow("SELECT report_json FROM terrain_report").Scan(&report)
	var metadata map[string]any
	if e = json.Unmarshal([]byte(report), &metadata); e != nil {
		t.Fatal(e)
	}
	if metadata["revision"] != float64(4) {
		t.Fatal("revision missing from report")
	}
}
func TestOfflineEPSGCatalogAndUnknownCoordinates(t *testing.T) {
	for _, code := range []int{4326, 32643, 32736, 3857, 27700, 2036} {
		wkt, e := lookupWKT(code)
		if e != nil || len(wkt) < 100 {
			t.Fatalf("invalid EPSG:%d: %v", code, e)
		}
	}
	g := contourPlane()
	c, e := generateContours(context.Background(), g, g.Z, ContourParams{Interval: 1, Base: .5, IndexEvery: 2, AverageFactor: 1}, 0, func(int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	b, e := contourGPKG(context.Background(), g, c)
	if e != nil || len(b) < 100 {
		t.Fatalf("undefined source coordinates should export without inventing a CRS: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := contourGPKG(ctx, g, c); e == nil {
		t.Fatal("cancelled export succeeded")
	}
}

func TestPixelIsPointContourGeoreferencing(t *testing.T) {
	g := areaFixture()
	keys := []uint16{1, 1, 0, 3, 1025, 0, 1, 2, 3076, 0, 1, 9001, 3072, 0, 1, 32643}
	data := make([]byte, len(keys)*2)
	for i, v := range keys {
		g.Order.PutUint16(data[i*2:], v)
	}
	g.Tags[34735] = tag{34735, 3, uint32(len(keys)), data}
	x, y, ok := worldAt(g, .5, .5)
	if !ok || x != 500000 || y != 1000000 {
		t.Fatal("PixelIsPoint shifted the cell centre")
	}
	p := ContourParams{Interval: 1, IndexEvery: 5, AverageFactor: 2}
	surface, e := contourSurface(context.Background(), g, g.Z, p)
	if e != nil {
		t.Fatal(e)
	}
	ax, ay, _ := worldAt(surface, .5, .5)
	bx, by, _ := worldAt(g, 1, 1)
	if ax != bx || ay != by {
		t.Fatal("averaging shifted PixelIsPoint georeferencing")
	}
	_, wkt, _, e := sourceSRS(g)
	if e != nil || len(wkt) < 100 {
		t.Fatal("projected EPSG CRS was not resolved")
	}
}

func TestCustomProjectedCRSDoesNotBecomeGeographic(t *testing.T) {
	g := areaFixture()
	keys := []uint16{1, 1, 0, 4, 1024, 0, 1, 1, 3072, 0, 1, 32767, 2048, 0, 1, 4326, 3076, 0, 1, 9001}
	data := make([]byte, len(keys)*2)
	for i, v := range keys {
		g.Order.PutUint16(data[i*2:], v)
	}
	g.Tags[34735] = tag{34735, 3, uint32(len(keys)), data}
	id, _, _, e := sourceSRS(g)
	if e != nil || id != -1 {
		t.Fatal("a custom projected CRS was incorrectly assigned its geographic base CRS")
	}
	g.Order.PutUint16(data[14:], 2)
	id, _, _, e = sourceSRS(g)
	if e != nil || id != 4326 {
		t.Fatal("a declared geographic CRS was not resolved")
	}
}
