import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';

const {chromium} = await import(process.env.PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.PLAYWRIGHT_MODULE).href : 'playwright');
const browser = await chromium.launch({
  executablePath:process.env.CHROMIUM_PATH || undefined, headless:true,
  args:process.env.CHROMIUM_NO_SANDBOX === '1' ? ['--no-sandbox'] : [],
});

// A small independent classic float32 TIFF: three spikes in a swept area, one
// outside, one NoData cell, and two more spikes for a circular single-click test.
function fixture() {
  const w=60, h=40, entries=10, offset=8+2+entries*12+4;
  const data=Buffer.alloc(offset+w*h*4);
  data.write('II'); data.writeUInt16LE(42,2); data.writeUInt32LE(8,4);
  data.writeUInt16LE(entries,8);
  const tags=[[256,4,w],[257,4,h],[258,3,32],[259,3,1],[262,3,1],
    [273,4,offset],[277,3,1],[278,4,h],[279,4,w*h*4],[339,3,3]];
  tags.forEach(([id,type,value],i) => {
    const at=10+i*12;
    data.writeUInt16LE(id,at); data.writeUInt16LE(type,at+2);
    data.writeUInt32LE(1,at+4); data.writeUInt32LE(value,at+8);
  });
  for(let i=0;i<w*h;i++) data.writeFloatLE(10,offset+i*4);
  for(const [x,y,z] of [[12,12,90],[28,13,-40],[40,12,100],
    [25,16,90],[20,12,NaN],[50,28,80],[52,28,-30]]) {
    data.writeFloatLE(z,offset+(y*w+x)*4);
  }
  return data;
}

try {
  const page=await browser.newPage({viewport:{width:1550,height:1000}});
  page.setDefaultTimeout(15000);
  const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('dialog',d=>d.accept());
  await page.goto(process.env.STUDIO_URL || 'http://127.0.0.1:8789');
  await page.locator('#contourTab').click();
  await page.locator('#autoContours').uncheck();
  await page.locator('#editTab').click();
  const chooser=page.waitForEvent('filechooser');
  await page.locator('#openDTM').click();
  await (await chooser).setFiles({name:'brush-fixture.tif',mimeType:'image/tiff',buffer:fixture()});
  await page.waitForFunction(()=>state.raster?.name==='brush-fixture.tif' && state.complete && !uploading && after);
  const at=(x,y)=>page.evaluate(({x,y})=>{
    const g=geometry(),r=surface.getBoundingClientRect();
    return {x:r.left+g.w/2+(x/state.raster.width-camera.x)*g.dw,
      y:r.top+g.h/2+(y/state.raster.height-camera.y)*g.dh};
  },{x,y});
  const inspect=(x,y)=>page.evaluate(async({x,y})=>(await api(`inspect?x=${x}&y=${y}`)).json(),{x,y});
  const move=async(x,y)=>{const p=await at(x,y);await page.mouse.move(p.x,p.y)};
  const settled=()=>page.waitForFunction(()=>!editingPending && !brushActive());
  await page.locator('[data-tool=spike]').click();
  await page.locator('#brushRadius').fill('2');
  await move(10.5,12.5);await page.mouse.down();
  // Sparse endpoints must cover the entire intervening footprint, including
  // high/low spikes offset from both the pointer path and its endpoints.
  await move(44.5,12.5);
  assert.equal(await page.evaluate(()=>state.editing.undo),0,'stroke applied before release');
  const cameraBefore=await page.evaluate(()=>({...camera}));
  await page.mouse.wheel(0,-200);
  assert.deepEqual(await page.evaluate(()=>({...camera})),cameraBefore,'zoom changed the brush footprint mid-stroke');
  await page.mouse.up();await settled();
  for(const [x,y] of [[12,12],[28,13],[40,12]]) assert.equal((await inspect(x,y)).dtm,10,'covered spike was missed');
  assert.equal((await inspect(25,16)).dtm,90,'outside-brush cell was changed');
  assert.equal((await inspect(20,12)).dtm,null,'brush filled NoData');
  assert.equal(await page.evaluate(()=>state.editing.undo),1,'stroke should create one Undo action');
  const report=await page.evaluate(async()=>(await api('report')).json());
  assert.equal(report.edits[0].cells,3);
  assert.equal(report.edits[0].radius,2);
  assert(report.edits[0].points.length>=2);
  await page.locator('#undo').click();await settled();
  assert.equal((await inspect(12,12)).dtm,90);
  assert.equal((await inspect(28,13)).dtm,-40);
  assert.equal((await inspect(40,12)).dtm,100);
  await page.locator('#redo').click();await settled();
  assert.equal((await inspect(28,13)).dtm,10);

  // Both cancellation paths leave the still-unrepaired outside spike intact.
  for(const cancel of ['escape','pointercancel']) {
    await move(21.5,16.5);await page.mouse.down();await move(29.5,16.5);
    if(cancel==='escape') await page.keyboard.press('Escape');
    else await page.evaluate(()=>surface.dispatchEvent(new PointerEvent('pointercancel',{pointerId:brushStroke.id})));
    await page.mouse.up();await settled();
    assert.equal((await inspect(25,16)).dtm,90,'cancelled stroke changed elevations');
    assert.equal(await page.evaluate(()=>state.editing.undo),1);
  }
  // Single click still repairs all covered cells, not only its centre.
  await page.locator('#brushRadius').fill('3');
  const p=await at(51.5,28.5);await page.mouse.click(p.x,p.y);await settled();
  assert.equal((await inspect(50,28)).dtm,10);
  assert.equal((await inspect(52,28)).dtm,10);
  assert.equal(await page.evaluate(()=>state.editing.undo),2);
  const clean=await at(5.5,28.5);await page.mouse.click(clean.x,clean.y);await settled();
  assert.equal(await page.evaluate(()=>state.editing.undo),2,'empty brush created undo history');
  assert.match(await page.locator('#message').textContent(),/No spikes exceeded/);
  assert.equal(await page.locator('#error').isVisible(),false);
  // Middle-drag still pans rather than applying another stroke.
  const beforePan=await page.evaluate(()=>({...camera}));
  await move(30,20);await page.mouse.down({button:'middle'});
  await page.mouse.move((await at(30,20)).x+30,(await at(30,20)).y);
  await page.mouse.up({button:'middle'});
  assert.notEqual(await page.evaluate(()=>camera.x),beforePan.x);
  assert.equal(await page.evaluate(()=>state.editing.undo),2);
  assert.deepEqual(errors,[]);
  console.log('PASS brush: full swept area, sparse-event coverage, high/low repairs, NoData/outside protection, one Undo/Redo, cancellation, single-click footprint, no-op feedback and middle-button pan.');
} finally {
  await browser.close();
}
