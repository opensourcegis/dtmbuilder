"""Development interoperability check only; not needed to run the Go application."""
from pathlib import Path
import numpy as np
import rasterio
from rasterio.transform import from_origin
import subprocess
import tempfile
import os

root = Path(__file__).resolve().parent
executable = Path(os.environ.get('TERRAIN_EXECUTABLE', root / 'dist' / ('Terrain-Studio.exe' if os.name == 'nt' else 'terrain-studio')))
with tempfile.TemporaryDirectory(dir=root) as tmp:
    tmp = Path(tmp)
    y, x = np.mgrid[:64, :64]
    z = (50 + x * .05 + y * .02).astype('float32')
    z[18:28, 18:28] += 10
    z[3, 3] = -9999
    for compression, predictor, tiled, endian in [('none',1,False,'LITTLE'), ('deflate',3,True,'LITTLE'), ('lzw',3,True,'LITTLE'), ('lzw',1,False,'BIG'), ('deflate',2,False,'LITTLE')]:
        label = f'{compression}-{predictor}-{endian}'
        src = tmp / f'{label}.tif'
        out = tmp / label
        with rasterio.open(src, 'w', driver='GTiff', width=64, height=64, count=1,
                           dtype='float32', crs='EPSG:32636', transform=from_origin(300000, 1000000, .1, .1),
                           nodata=-9999, compress=compression, predictor=predictor,
                           tiled=tiled, blockxsize=16 if tiled else 64, blockysize=16,
                           ENDIANNESS=endian) as ds:
            ds.write(z, 1)
        run = subprocess.run([str(executable), '-input', str(src), '-output', str(out)], capture_output=True, text=True)
        if run.returncode:
            raise RuntimeError(f'{label}: {run.stderr}')
        with rasterio.open(out/'dtm.tif') as ds:
            a = ds.read(1)
            assert ds.crs.to_epsg() == 32636
            assert ds.transform == from_origin(300000, 1000000, .1, .1)
            assert np.isnan(a[3,3]) and np.isfinite(a[0,0])
            assert a[23,23] < z[23,23] - 5
        print(f'PASS {label}: elevation, CRS, transform, NoData, building removal')
