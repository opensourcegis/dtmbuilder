// Independent browser workflow for drawn breaklines, water polygons and islands.
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE?pathToFileURL(process.env.PLAYWRIGHT_MODULE).href:'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH||undefined,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
function fixture(){
 const w=480,h=480,n=13,base=8+2+n*12+4,scale=base,tie=scale+24,keys=tie+48,pixels=keys+32;
 const b=Buffer.alloc(pixels+w*h*4);b.write('II');b.writeUInt16LE(42,2);b.writeUInt32LE(8,4);b.writeUInt16LE(n,8);
 const tags=[[256,4,1,w],[257,4,1,h],[258,3,1,32],[259,3,1,1],[262,3,1,1],[273,4,1,pixels],[277,3,1,1],[278,4,1,h],[279,4,1,w*h*4],[339,3,1,3],[33550,12,3,scale],[33922,12,6,tie],[34735,3,16,keys]];
 tags.forEach(([id,type,count,value],i)=>{const p=10+i*12;b.writeUInt16LE(id,p);b.writeUInt16LE(type,p+2);b.writeUInt32LE(count,p+4);b.writeUInt32LE(value,p+8)});
 [1,1,0].forEach((v,i)=>b.writeDoubleLE(v,scale+i*8));[0,0,0,500000,2000000,0].forEach((v,i)=>b.writeDoubleLE(v,tie+i*8));
 [1,1,0,3,1024,0,1,1,3072,0,1,32643,3076,0,1,9001].forEach((v,i)=>b.writeUInt16LE(v,keys+i*2));
 for(let y=0;y<h;y++)for(let x=0;x<w;x++){const noise=(x+y)%2?.025:-.025,hill=6*Math.exp(-((x-240)**2+(y-225)**2)/180);b.writeFloatLE(30+x*.02+y*.01+noise+hill,pixels+(y*w+x)*4)}
 return b;
}
try{
 const page=await browser.newPage({viewport:{width:1550,height:1000}});page.setDefaultTimeout(20000);const errors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
 await page.goto(process.env.STUDIO_URL||'http://127.0.0.1:8789');await page.locator('#contourTab').click();await page.locator('#autoContours').uncheck();
 await page.locator('#contourHeightUnit').selectOption('m');await page.locator('#contourPreset').selectOption('constrained');
 const choosing=page.waitForEvent('filechooser');await page.locator('#openDTM').click();await(await choosing).setFiles({name:'constrained-plane-island.tif',mimeType:'image/tiff',buffer:fixture()});
 await page.waitForFunction(()=>state.complete&&!uploading&&after);await page.locator('#contourTab').click();await page.locator('#autoContours').check();
 const ready=()=>page.waitForFunction(()=>!editingPending&&!state.contourBusy&&state.contours?.revision===state.editing.revision&&contourData?.generation===state.contours.generation);
 await ready();
 const at=(x,y)=>page.evaluate(({x,y})=>{const g=geometry(),r=surface.getBoundingClientRect();return {x:r.left+g.w/2+(x/state.raster.width-camera.x)*g.dw,y:r.top+g.h/2+(y/state.raster.height-camera.y)*g.dh}},{x,y});
 const clickPoints=async points=>{for(const [x,y] of points){const p=await at(x,y);await page.mouse.click(p.x,p.y)}};
 const inspect=(x,y)=>page.evaluate(async({x,y})=>(await api(`inspect?x=${x}&y=${y}`)).json(),{x,y});
 const beforePreserve=await inspect(100,90),beforeIsland=await inspect(240,225);
 await page.locator('[data-tool=breakline]').click();await clickPoints([[100.5,60.5],[100.5,130.5]]);await page.locator('#applySelection').click();await ready();
 assert.equal((await inspect(100,90)).dtm,beforePreserve.dtm);assert((await inspect(100,90)).flags&512);
 assert.equal(await page.locator('#contourSmooth').inputValue(),'0');assert.equal(await page.locator('#regionalRadius').inputValue(),'0');
 await page.locator('#breaklineMode').selectOption('stream');await page.locator('#breaklineStart').fill('32');await page.locator('#breaklineEnd').fill('31');
 await clickPoints([[80.5,70.5],[80.5,120.5]]);await page.locator('#applySelection').click();await ready();
 assert(Math.abs((await inspect(80,70)).dtm-32)<1e-5);assert(Math.abs((await inspect(80,120)).dtm-31)<1e-5);
 // Add the outer water ring first, then the island exclusion before committing.
 await page.locator('[data-tool=waterbody]').click();await page.locator('#waterLevel').fill('36');
 await clickPoints([[180,150],[300,150],[300,300],[180,300]]);await page.locator('#addIslandRing').click();
 await clickPoints([[220,205],[260,205],[260,245],[220,245]]);await page.locator('#applySelection').click();await ready();
 await page.waitForFunction(()=>terrainFeatureData.length===3&&featurePaths.length===3);
 assert.equal((await inspect(190,160)).dtm,36);assert((await inspect(190,160)).flags&1024);
 assert.equal((await inspect(240,225)).dtm,beforeIsland.dtm,'waterbody flattened its island');
 const details=await page.evaluate(()=>({features:state.terrainFeatures,q:state.contours.quality,performance:state.contours.performance,clipped:state.contours.waterClippedSegments}));
 assert.equal(details.features.length,3);assert.equal(details.q.featureSampleMismatches,0);assert(details.q.protectedFeatureCells>10000);assert(details.clipped>0);
 const validation=await page.evaluate(()=>{
  let waterInteriors=0,islandContours=0;
  for(const line of contourData.lines){
   if(line.elevation>39&&line.points.every(p=>p.x>220&&p.x<260&&p.y>205&&p.y<245))islandContours++;
   for(let i=1;i<line.points.length;i++){const a=line.points[i-1],b=line.points[i],x=(a.x+b.x)/2,y=(a.y+b.y)/2;if(x>180+1e-7&&x<300-1e-7&&y>150+1e-7&&y<300-1e-7&&!(x>=220-1e-7&&x<=260+1e-7&&y>=205-1e-7&&y<=245+1e-7))waterInteriors++}
  }
  return {waterInteriors,islandContours};
 });assert.equal(validation.waterInteriors,0);assert(validation.islandContours>0,'island contours were clipped away');
 // Existing terrain tools cannot silently overwrite locked water.
 const revision=await page.evaluate(()=>state.editing.revision);await page.locator('[data-tool=flatten]').click();await page.locator('#waterLevel').fill('50');
 await clickPoints([[185,155],[195,155],[195,165],[185,165]]);await page.locator('#applySelection').click();await page.waitForFunction(()=>!editingPending);
 assert.match(await page.locator('#error').textContent(),/protected breakline or waterbody/);assert.equal(await page.evaluate(()=>state.editing.revision),revision);
 await page.evaluate(()=>{clearError();clearSelection()});await page.locator('[data-tool=waterbody]').click();
 const waterID=details.features.find(f=>f.kind==='waterbody').id;
 await page.locator('#terrainFeatureList button').filter({hasText:`Remove #${waterID}`}).click();await ready();assert.equal((await inspect(190,160)).flags&1024,0);
 await page.locator('#undo').click();await ready();assert((await inspect(190,160)).flags&1024);assert.equal(await page.evaluate(()=>state.terrainFeatures.length),3);
 await page.locator('#contourTab').click();const downloading=page.waitForEvent('download');await page.locator('#saveGpkg').click();const output=await downloading;
 await mkdir('.tmp',{recursive:true});await output.saveAs('.tmp/constrained-terrain.gpkg');
 const terrainDownload=page.waitForEvent('download');await page.locator('#saveDTM').click();await(await terrainDownload).saveAs('.tmp/constrained-terrain-dtm.tif');
 await page.locator('[data-tool=pan]').click();await page.locator('#contourTab').click();await page.screenshot({path:'.tmp/constrained-terrain-preview.png',fullPage:true});
 assert.deepEqual(errors,[]);
 console.log(JSON.stringify({passed:'drawn preserve/graded breaklines, locked waterbody with island, no water-interior contours, atomic conflict rejection, remove/Undo and constraint GeoPackage export',...validation,clipped:details.clipped,protected:details.q.protectedFeatureCells}));
}finally{await browser.close()}
