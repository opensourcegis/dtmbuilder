import assert from 'node:assert/strict';
import {mkdir,readFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE?pathToFileURL(process.env.PLAYWRIGHT_MODULE).href:'playwright');
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||undefined,headless:true,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
function fixture(){
 const w=480,h=480,n=13,base=8+2+n*12+4,scale=base,tie=scale+24,keys=tie+48,pixels=keys+32;
 const b=Buffer.alloc(pixels+w*h*4);b.write('II');b.writeUInt16LE(42,2);b.writeUInt32LE(8,4);b.writeUInt16LE(n,8);
 const tags=[[256,4,1,w],[257,4,1,h],[258,3,1,32],[259,3,1,1],[262,3,1,1],[273,4,1,pixels],[277,3,1,1],[278,4,1,h],[279,4,1,w*h*4],[339,3,1,3],[33550,12,3,scale],[33922,12,6,tie],[34735,3,16,keys]];
 tags.forEach(([id,type,count,value],i)=>{const p=10+i*12;b.writeUInt16LE(id,p);b.writeUInt16LE(type,p+2);b.writeUInt32LE(count,p+4);b.writeUInt32LE(value,p+8)});
 [1,1,0].forEach((v,i)=>b.writeDoubleLE(v,scale+i*8));[0,0,0,500000,2000000,0].forEach((v,i)=>b.writeDoubleLE(v,tie+i*8));
 [1,1,0,3,1024,0,1,1,3072,0,1,32643,3076,0,1,9001].forEach((v,i)=>b.writeUInt16LE(v,keys+i*2));
 for(let i=0;i<w*h;i++)b.writeFloatLE(30+(i%w)*.02,pixels+i*4);
 b.writeFloatLE(NaN,pixels);return b;
}
try{
 const page=await browser.newPage({viewport:{width:1550,height:1000}});page.setDefaultTimeout(20000);const errors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
 await page.goto(process.env.STUDIO_URL||'http://127.0.0.1:8789');
 await page.locator('#contourTab').click();await page.locator('#autoContours').uncheck();
 const chooser=page.waitForEvent('filechooser');await page.locator('#openDTM').click();await(await chooser).setFiles({name:'live-plane.tif',mimeType:'image/tiff',buffer:fixture()});
 await page.waitForFunction(()=>state.raster?.name==='live-plane.tif'&&state.complete&&!uploading&&after);
 await page.locator('#contourHeightUnit').selectOption('m');await page.locator('#contourPreset').selectOption('cartographic');
 await page.locator('#contourBase').fill('.25');await page.locator('#contourBase').press('Tab');
 await page.locator('#contourTab').click();await page.locator('#autoContours').check();
 const ready=()=>page.waitForFunction(()=>!state.contourBusy&&state.contours?.revision===state.editing?.revision&&contourData?.generation===state.contours?.generation);
 await ready();
 const at=(x,y)=>page.evaluate(({x,y})=>{const g=geometry(),r=surface.getBoundingClientRect();return{x:r.left+g.w/2+(x/state.raster.width-camera.x)*g.dw,y:r.top+g.h/2+(y/state.raster.height-camera.y)*g.dh}},{x,y});
 const chosen=await page.evaluate(()=>{const line=contourData.lines.find(l=>l.elevation>34&&l.elevation<36),vertex=Math.floor(line.points.length/2);return{line:line.id,vertex,point:line.points[vertex],elevation:line.elevation}});
 await page.locator('[data-tool=contour]').click();let p=await at(chosen.point.x,chosen.point.y);await page.mouse.move(p.x,p.y);await page.mouse.down();p=await at(chosen.point.x+4,chosen.point.y);await page.mouse.move(p.x,p.y);await page.mouse.up();
 await page.waitForFunction(()=>!editingPending&&state.constraints?.length===1);await ready();
 const pin=await page.evaluate(()=>state.constraints[0]);assert(Math.abs(pin.elevation-chosen.elevation)<1e-8);
 assert.equal(await page.evaluate(()=>state.busy),false,'contour refresh blocked terrain editing');
 const distance=await page.evaluate(pin=>{
  let best=Infinity;
  for(const line of contourData.lines){if(line.elevation!==pin.elevation)continue;for(let i=1;i<line.points.length;i++){const a=line.points[i-1],b=line.points[i],dx=b.x-a.x,dy=b.y-a.y,l=dx*dx+dy*dy,t=l?Math.max(0,Math.min(1,((pin.x-a.x)*dx+(pin.y-a.y)*dy)/l)):0;best=Math.min(best,Math.hypot(pin.x-a.x-t*dx,pin.y-a.y-t*dy))}}
  return best;
 },pin);assert(distance<.01,'regenerated contour misses its pinned elevation location');
 // A DTM polygon edit triggers a background incremental update without pressing Generate.
 await page.locator('[data-tool=flatten]').click();await page.locator('#waterLevel').fill('32.5');
 for(const [x,y] of [[30,100],[45,100],[45,115],[30,115]]){p=await at(x,y);await page.mouse.click(p.x,p.y)}
 const started=Date.now();await page.locator('#applySelection').click();await page.waitForFunction(()=>!editingPending&&state.editing.undo===2);await ready();
 assert((await page.evaluate(()=>state.contours.performance.tilesReused))>0,'DTM edit rebuilt unaffected tiles');
 assert.equal(await page.evaluate(()=>state.contours.performance.mode),'incremental');
 const revision=await page.evaluate(()=>state.editing.revision);
 // Conflicting edits cannot overwrite a pinned elevation.
 await page.locator('#waterLevel').fill('100');
 for(const [x,y] of [[pin.x-2,pin.y-2],[pin.x+2,pin.y-2],[pin.x+2,pin.y+2],[pin.x-2,pin.y+2]]){p=await at(x,y);await page.mouse.click(p.x,p.y)}
 await page.locator('#applySelection').click();await page.waitForFunction(()=>!editingPending);
 assert.match(await page.locator('#error').textContent(),/pinned contour/);assert.equal(await page.evaluate(()=>state.editing.revision),revision);
 await page.evaluate(()=>{clearError();clearSelection()});await page.locator('[data-tool=contour]').click();
 await page.locator('#constraintList button').click();await page.waitForFunction(()=>!editingPending&&state.constraints.length===0);await ready();
 await page.locator('#undo').click();await page.waitForFunction(()=>!editingPending&&state.constraints.length===1);await ready();
 await page.locator('#contourTab').click();const dl=page.waitForEvent('download');await page.locator('#saveGpkg').click();const file=await dl;assert.equal(file.suggestedFilename(),'contours.gpkg');
 await mkdir('.tmp',{recursive:true});await file.saveAs('.tmp/live-contours.gpkg');
 const metadata=await page.evaluate(()=>({revision:state.editing.revision,lines:state.contours.lines,constraints:state.constraints}));
 const bytes=await readFile('.tmp/live-contours.gpkg');assert.equal(bytes.subarray(0,15).toString(),'SQLite format 3');
 assert.equal(bytes.readUInt32BE(68),0x47504b47);assert.deepEqual(errors,[]);
 console.log(JSON.stringify({passed:'live contour drag, pinned DTM, automatic incremental refresh, edit protection, unpin/Undo and GeoPackage export',revision:metadata.revision,lines:metadata.lines,pins:metadata.constraints.length,polygonUpdateAndChecksMs:Date.now()-started}));
}finally{await browser.close()}
