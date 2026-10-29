from pathlib import Path
import numpy as np
import rasterio
from rasterio.transform import from_origin
folder=Path(__file__).parent/'.tmp'
folder.mkdir(exist_ok=True)
y,x=np.mgrid[:61,:61]
z=np.maximum(np.maximum(0,6-2*np.maximum(abs(x-15),abs(y-15))),np.maximum(0,12-np.maximum(abs(x-40),abs(y-40)))).astype('float32')
with rasterio.open(folder/'area-input.tif','w',driver='GTiff',width=61,height=61,count=1,dtype='float32',crs='EPSG:32636',transform=from_origin(500000,1000000,1,1)) as ds:
    ds.write(z,1)
