"""Optional independent GDAL/Fiona check of live_contours_check.mjs output."""
from pathlib import Path
import json
import shutil
import sqlite3
import fiona

path = Path(__file__).parent / '.tmp' / 'live-contours.gpkg'
with sqlite3.connect(path) as db:
    assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    assert not db.execute('PRAGMA foreign_key_check').fetchall()
    report = json.loads(db.execute('SELECT report_json FROM terrain_report').fetchone()[0])
    assert len(report['constraints']) == 1
    assert db.execute('SELECT count(*) FROM contours').fetchone()[0] == db.execute('SELECT count(*) FROM rtree_contours_geom').fetchone()[0]
with fiona.open(path, layer='contours') as ds:
    assert ds.crs.to_epsg() == 32643
    assert ds.schema['geometry'] == 'LineString'
    features = list(ds)
    assert len(features) == report['lines'] > 0
    assert all(f['properties']['elevation_unit'] == 'm' and f['properties']['revision'] == report['revision'] for f in features)
    assert all(f['properties']['depression'] is None for f in features)
    first = features[0]
copy = path.with_name('gpkg-edit-check.gpkg')
shutil.copy2(path, copy)
with fiona.open(copy, 'a', layer='contours') as ds:
    ds.write({'geometry': dict(first['geometry']), 'properties': dict(first['properties'])})
with sqlite3.connect(copy) as db:
    assert db.execute('SELECT count(*) FROM contours').fetchone()[0] == db.execute('SELECT count(*) FROM rtree_contours_geom').fetchone()[0] == len(features) + 1
print('PASS independent GIS: EPSG/geometry/attributes/revision, SQLite integrity, constraint metadata and spatial-index maintenance after append')
