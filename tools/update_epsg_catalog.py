"""Development-only: build the offline EPSG WKT catalog from Rasterio's PROJ data."""
import gzip
import json
import sqlite3
from pathlib import Path
import rasterio

root = Path(__file__).resolve().parents[1]
db = sqlite3.connect(Path(rasterio.__file__).parent / 'proj_data' / 'proj.db')
codes = db.execute("SELECT code FROM projected_crs WHERE auth_name='EPSG' UNION SELECT code FROM geodetic_crs WHERE auth_name='EPSG' AND type='geographic 2D' ORDER BY code").fetchall()
result = {}
# Preserve deprecated definitions as declared; never substitute a different CRS
# while retaining the original EPSG identifier in the exported GeoPackage.
with rasterio.Env(OSR_USE_NON_DEPRECATED='NO'):
    for (code,) in codes:
        try:
            crs = rasterio.CRS.from_epsg(code)
            unit = 'degrees' if crs.is_geographic else 'map units'
            if crs.is_projected:
                _, factor = crs.linear_units_factor
                for value, label in [(1, 'm'), (.3048, 'ft'), (1200/3937, 'US ft')]:
                    if abs(factor-value) < 1e-12:
                        unit = label
            result[str(code)] = {'wkt': crs.to_wkt(), 'unit': unit}
        except rasterio.errors.CRSError:
            pass
payload = json.dumps(result, sort_keys=True, separators=(',', ':')).encode()
(root / 'data' / 'epsg-wkt.json.gz').write_bytes(gzip.compress(payload, mtime=0))
meta = dict(db.execute('SELECT key,value FROM metadata'))
(root / 'data' / 'epsg-catalog.json').write_text(json.dumps({
    'source': 'PROJ proj.db distributed with Rasterio',
    'proj_version': meta['PROJ.VERSION'], 'epsg_version': meta['EPSG.VERSION'],
    'epsg_date': meta['EPSG.DATE'], 'crs_count': len(result),
    'license': 'PROJ-COPYING; EPSG coordinate reference system definitions attributed to IOGP/EPSG',
}, indent=2) + '\n')
print(f'Wrote {len(result)} CRS definitions, {len(gzip.compress(payload, mtime=0))} compressed bytes')
