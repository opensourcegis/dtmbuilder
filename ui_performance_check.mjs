// Optional browser regression for loading, response races, cancellation and rendering.
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE?pathToFileURL(process.env.PLAYWRIGHT_MODULE).href:'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH||undefined,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
let releaseDelayed=()=>{},page;
try{
 page=await browser.newPage({viewport:{width:1550,height:1000}});page.setDefaultTimeout(20000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));let acceptReplacement=true,dialogs=0;
 page.on('dialog',async d=>{dialogs++;if(acceptReplacement)await d.accept();else await d.dismiss()});
 await page.goto(process.env.STUDIO_URL||'http://127.0.0.1:8789');
 await page.locator('#demo').click();await page.waitForFunction(()=>state.ready&&!uploading&&before);
 await page.locator('#editBase').click();
 await page.waitForFunction(()=>state.contours&&!state.contourBusy&&contourData?.generation===state.contours.generation);
 assert.equal(await page.locator('#contourMinArea').inputValue(),'0','default area filter failed for unreferenced demo');
 assert.equal(await page.locator('#error').isVisible(),false);
 // Rendering bursts should paint once without reallocating the existing canvas.
 const painting=await page.evaluate(async()=>{
  await new Promise(requestAnimationFrame);let paints=0,resizes=0;
  const overlay=drawOverlay;drawOverlay=(...args)=>{paints++;overlay(...args)};
  const observer=new MutationObserver(records=>{resizes+=records.filter(r=>['width','height'].includes(r.attributeName)).length});
  observer.observe(surface,{attributes:true});
  for(let i=0;i<120;i++)draw();
  await new Promise(requestAnimationFrame);await new Promise(requestAnimationFrame);
  observer.disconnect();drawOverlay=overlay;return {paints,resizes};
 });assert.deepEqual(painting,{paints:1,resizes:0});
 const preparation=await page.evaluate(async()=>{
  const c={revision:state.editing.revision,generation:state.contours.generation,lines:[{id:0,points:Array.from({length:400000},(_,i)=>({x:i%1000,y:Math.floor(i/1000)}))}]};
  let yielded=false;const timer=setTimeout(()=>{yielded=true},0),started=performance.now();
  const paths=await cacheContourPaths(c,contourFetchToken);clearTimeout(timer);
  return {vertices:c.lines[0].points.length,milliseconds:Math.round(performance.now()-started),yielded,bounds:[paths[0].minx,paths[0].miny,paths[0].maxx,paths[0].maxy]};
 });assert(preparation.yielded,'large contour preparation blocked all UI callbacks');assert.deepEqual(preparation.bounds,[0,0,999,399]);
 await page.locator('#contourTab').click();await page.locator('#autoContours').uncheck();
 await page.locator('#contourHeightUnit').selectOption('m');await page.locator('#contourPreset').selectOption('cartographic');
 await page.locator('#contourHeightUnit').selectOption('ft');assert.equal(await page.locator('#contourQuantum').inputValue(),'0.1');
 assert.equal(await page.locator('#contourPreset').inputValue(),'cartographic');
 await page.locator('#contourMinArea').fill('123');await page.locator('#contourMinArea').press('Tab');
 await page.locator('#contourPreset').selectOption('local');assert.equal(await page.locator('#contourMinArea').inputValue(),'0');
 // An older Identify response arriving last must not replace the newer cell.
 let firstReceived;const received=new Promise(r=>{firstReceived=r});const delayed=new Promise(r=>{releaseDelayed=r});
 await page.route('**/api/inspect?x=10&y=10',async route=>{const response=await route.fetch();firstReceived();await delayed;await route.fulfill({response})});
 await page.evaluate(()=>{void identifyCell({x:10.5,y:10.5})});await received;
 await page.evaluate(()=>identifyCell({x:20.5,y:20.5}));releaseDelayed();
 await page.waitForResponse(r=>r.url().endsWith('/api/inspect?x=10&y=10'));
 assert.equal(await page.evaluate(()=>lastInspection.column),20);
 await page.unroute('**/api/inspect?x=10&y=10');
 // History DOM remains stable between polling responses when no edit occurred.
 await page.evaluate(()=>submitEdit({tool:'flatten',points:[{x:30,y:30},{x:35,y:30},{x:35,y:35},{x:30,y:35}],elevation:52}));
 await page.waitForFunction(()=>state.unsavedEdits&&!editingPending);
 const historyNode=await page.locator('#editHistory div').elementHandle();await page.waitForTimeout(600);
 assert(await historyNode.evaluate(n=>n.isConnected),'polling rebuilt unchanged edit history');
 // Delay an inspector result, change the terrain revision, then reject the stale height.
 let editReceived;const editResponseReady=new Promise(r=>{editReceived=r});const editDelayed=new Promise(r=>{releaseDelayed=r});
 await page.route('**/api/inspect?x=11&y=11',async route=>{const response=await route.fetch();editReceived();await editDelayed;await route.fulfill({response})});
 await page.evaluate(()=>{void identifyCell({x:11.5,y:11.5})});await editResponseReady;
 await page.evaluate(()=>historyAction(false));releaseDelayed();
 await page.waitForResponse(r=>r.url().endsWith('/api/inspect?x=11&y=11'));
 assert.equal(await page.evaluate(()=>lastInspection),null);assert(await page.locator('#useHeight').isDisabled());
 await page.unroute('**/api/inspect?x=11&y=11');await page.evaluate(()=>historyAction(true));
 const fixture=Buffer.from(await page.evaluate(async()=>Array.from(new Uint8Array(await(await api('download?layer=dsm')).arrayBuffer()))));
 const previousVersion=await page.evaluate(()=>version);
 const chooserPromise=page.waitForEvent('filechooser');await page.locator('#openDTM').click();const chooser=await chooserPromise;
 await chooser.setFiles([]);await page.evaluate(()=>$('file').dispatchEvent(new Event('cancel')));
 assert.equal(await page.evaluate(()=>openAsDTM),false,'cancelled DTM picker leaked its mode');
 // Drop must ask before discarding edits, and must open a DSM after a cancelled DTM picker.
 const drop=async()=>page.evaluate(bytes=>{const transfer=new DataTransfer();transfer.items.add(new File([new Uint8Array(bytes)],'dropped.tif',{type:'image/tiff'}));$('drop').dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:transfer}))},Array.from(fixture));
 acceptReplacement=false;const oldDialogs=dialogs;await drop();await page.waitForTimeout(200);
 assert(dialogs>oldDialogs,'drop bypassed unsaved-edit confirmation');assert.equal(await page.evaluate(()=>version),previousVersion);
 assert(await page.evaluate(()=>state.unsavedEdits));
 acceptReplacement=true;await drop();await page.waitForFunction(()=>state.raster?.name==='dropped.tif'&&!uploading);
 assert.equal(await page.evaluate(()=>state.complete),false,'drop inherited cancelled DTM import mode');
 for(let i=0;i<2;i++){
  const oldVersion=await page.evaluate(()=>version);const choosing=page.waitForEvent('filechooser');await page.locator('#openDTM').click();
  await(await choosing).setFiles({name:'same input – भू.tif',mimeType:'image/tiff',buffer:fixture});
  await page.waitForFunction(old=>version>old&&state.complete&&!uploading,oldVersion);
  assert.equal(await page.locator('#file').inputValue(),'','same-file reselect was not reset');
  assert.equal(await page.evaluate(()=>state.raster.name),'same input – भू.tif','uploaded filename was left URL-encoded');
 }
 // Hold state responses while the job starts, as on a slow connection, and use the visible Cancel control.
 await page.locator('#contourTab').click();await page.locator('#autoContours').uncheck();
 let releaseState;const heldState=new Promise(r=>{releaseState=r});releaseDelayed=releaseState;
 await page.route('**/api/state',async route=>{const response=await route.fetch();await heldState;await route.fulfill({response})});
 const job=page.waitForRequest(r=>r.url().endsWith('/api/contours/generate'));
 await page.locator('#generateContours').click();await job;await page.waitForFunction(()=>state.contourBusy);
 assert(await page.locator('#cancelContours').isEnabled());const cancelling=page.waitForRequest(r=>r.url().endsWith('/api/cancel'));
 await page.locator('#cancelContours').click();await cancelling;releaseState();await page.unrouteAll({behavior:'wait'});
 await page.waitForFunction(()=>!state.contourBusy);
 // Compact browser windows must keep the viewer, footer and toolbar reachable.
 await page.setViewportSize({width:900,height:700});await page.waitForTimeout(200);
 const layout=await page.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth,viewerBottom:$('viewer').getBoundingClientRect().bottom,footerTop:document.querySelector('footer').getBoundingClientRect().top,toolbarOverflow:getComputedStyle(document.querySelector('.main-tools')).overflowX}));
 assert.equal(layout.overflow,false);assert(layout.viewerBottom<=layout.footerTop);assert.equal(layout.toolbarOverflow,'auto');
 await page.locator('#layer').selectOption('flags');assert.match(await page.locator('#legend').textContent(),/pink = contour pin/);
 await mkdir('.tmp',{recursive:true});await page.screenshot({path:'.tmp/ui-review-compact.png',fullPage:true});
 assert.deepEqual(errors,[]);
 console.log(JSON.stringify({passed:'default contours, unit/preset consistency, stale inspector rejection, stable history, cancelled/same-file imports, guarded drop, visible cancellation and compact layout',painting,preparation}));
}finally{releaseDelayed();if(page)await page.unrouteAll({behavior:'wait'});await browser.close()}
