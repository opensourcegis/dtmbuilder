"""Independent GIS validation of constrained_terrain_check.mjs outputs (development only)."""
from pathlib import Path
import json
import sqlite3
import fiona
import numpy as np
import rasterio
from shapely.geometry import shape

root = Path(__file__).parent / '.tmp'
package = root / 'constrained-terrain.gpkg'
with sqlite3.connect(package) as db:
    assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    assert not db.execute('PRAGMA foreign_key_check').fetchall()
    report = json.loads(db.execute('SELECT report_json FROM terrain_report').fetchone()[0])
    assert len(report['terrainFeatures']) == 3
    assert report['quality']['featureSampleMismatches'] == 0
    assert report['waterClippedSegments'] > 0
with fiona.open(package, layer='terrain_waterbodies') as ds:
    assert ds.crs.to_epsg() == 32643
    features = list(ds)
    assert len(features) == 1
    water = shape(features[0]['geometry'])
    assert water.is_valid and len(water.interiors) == 1
    assert abs(water.area - (120 * 150 - 40 * 40)) < 0.05
    assert features[0]['properties']['start_height'] == 36
with fiona.open(package, layer='terrain_breaklines') as ds:
    features = list(ds)
    assert ds.crs.to_epsg() == 32643 and len(features) == 2
    graded = next(f for f in features if f['properties']['mode'] == 'stream')
    assert graded['properties']['start_height'] == 32
    assert graded['properties']['end_height'] == 31
    preserved = next(f for f in features if f['properties']['mode'] == 'preserve')
    assert preserved['properties']['start_height'] is None
with fiona.open(package, layer='contours') as ds:
    assert ds.crs.to_epsg() == 32643
    interior = water.buffer(-1e-6)
    count = 0
    for feature in ds:
        line = shape(feature['geometry'])
        assert line.is_valid
        assert interior.intersection(line).length < 1e-7, 'exported contour enters water interior'
        count += 1
    assert count == report['lines']
with rasterio.open(root / 'constrained-terrain-dtm.tif') as ds:
    assert ds.crs.to_epsg() == 32643
    z = ds.read(1)
    mask = np.zeros(z.shape, dtype=bool)
    mask[150:300, 180:300] = True
    mask[205:245, 220:260] = False
    assert np.all(z[mask] == np.float32(36)), 'saved DTM lost its known water level'
    assert z[225, 240] > 42, 'saved DTM lost its island'
    assert abs(float(z[70, 80]) - 32) < 1e-5 and abs(float(z[120, 80]) - 31) < 1e-5
print('PASS independent GIS: valid constraint geometries/CRS/island, endpoint attributes, zero water-interior contour length, and saved DTM heights')
