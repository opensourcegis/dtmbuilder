// Regression: replacement imports must block edits and clear old contour overlays.
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE?pathToFileURL(process.env.PLAYWRIGHT_MODULE).href:'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH||undefined,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
let release=()=>{};
try{
 const page=await browser.newPage({viewport:{width:1550,height:1000}});page.setDefaultTimeout(20000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
 await page.goto(process.env.STUDIO_URL||'http://127.0.0.1:8789');
 await page.locator('#demo').click();await page.waitForFunction(()=>state.ready&&!uploading&&before);
 await page.locator('#editBase').click();
 await page.waitForFunction(()=>state.contours&&!state.contourBusy&&contourData?.generation===state.contours.generation);
 await page.evaluate(()=>{$('autoContours').checked=false;$('autoContours').onchange()});
 const fixture=await page.evaluate(async()=>Array.from(new Uint8Array(await(await api('download?layer=dsm')).arrayBuffer())));
 await page.evaluate(()=>submitEdit({tool:'flatten',points:[{x:30,y:30},{x:35,y:30},{x:35,y:35},{x:30,y:35}],elevation:52}));
 await page.evaluate(()=>makeContours());await page.waitForFunction(()=>state.contours&&!state.contourBusy&&contourData?.generation===state.contours.generation);
 await page.evaluate(()=>{setTool('flatten');polygon=[{x:30,y:30},{x:35,y:30},{x:35,y:35},{x:30,y:35}];updateSelection()});
 let received;const receivedUpload=new Promise(r=>{received=r}),heldUpload=new Promise(r=>{release=r});
 await page.route('**/api/upload?mode=dtm',async route=>{const response=await route.fetch();received();await heldUpload;await route.fulfill({response})});
 let editingRequests=0;page.on('request',r=>{if(r.method()==='POST'&&/\/api\/(undo|redo|edit|terrain-features\/remove|contours\/unpin)$/.test(new URL(r.url()).pathname))editingRequests++});
 await page.evaluate(bytes=>{void upload(new File([new Uint8Array(bytes)],'replacement.tif',{type:'image/tiff'}),true,true)},fixture);
 await receivedUpload;
 const findings=[];
 if(!await page.locator('#applySelection').isDisabled())findings.push('Apply selection remains enabled during replacement');
 await page.locator('#canvas').focus();await page.keyboard.press('Control+z');
 await page.waitForTimeout(250);
 if(editingRequests!==0)findings.push('Undo shortcut submitted during replacement');
 await page.evaluate(()=>setTool('spike'));
 await page.locator('#canvas').dispatchEvent('pointerdown',{button:0,pointerId:1,clientX:800,clientY:450});
 if(await page.evaluate(()=>brushActive()))findings.push('Brush gesture started during replacement');
 await page.evaluate(()=>cancelBrushStroke());
 release();await page.waitForFunction(()=>!uploading&&state.raster?.name==='replacement.tif');
 if(await page.evaluate(()=>contourData!==null||contourPaths.length!==0))findings.push('Previous raster contours survived replacement with auto refresh off');
 assert.deepEqual(findings,[]);assert.deepEqual(errors,[]);
 console.log(JSON.stringify({passed:'replacement blocks Apply, Undo shortcuts and brush gestures; old contour overlays cleared with auto refresh off'}));
}finally{release();await browser.close()}
