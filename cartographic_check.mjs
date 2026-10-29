// Optional browser regression. Install Playwright separately; no npm runtime is
// needed by Terrain Studio. PLAYWRIGHT_MODULE and CHROMIUM_PATH support CI tools.
import assert from 'node:assert/strict';
import {mkdir, readFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';

const {chromium} = await import(process.env.PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.PLAYWRIGHT_MODULE).href : 'playwright');
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_PATH || undefined,
  headless: true,
  args: process.env.CHROMIUM_NO_SANDBOX === '1' ? ['--no-sandbox'] : [],
});
try {
  const page = await browser.newPage({viewport: {width: 1550, height: 1000}});
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('dialog', d => d.accept());
  await page.goto(process.env.STUDIO_URL || 'http://127.0.0.1:8789');
  await page.locator('#contourTab').click();
  await page.locator('#autoContours').uncheck();
  await page.locator('#contourHeightUnit').selectOption('m');
  await page.locator('#contourPreset').selectOption('cartographic');
  assert.equal(await page.locator('#contourQuantum').inputValue(), '0.03048');
  assert.equal(await page.locator('#contourMinArea').inputValue(), '0');
  await page.locator('#demo').click();
  await page.locator('#editBase').click();
  await page.waitForFunction(() => state.complete && !state.busy);
  await page.locator('#contourTab').click();
  const inspect = () => page.evaluate(async () => (await api('inspect?x=60&y=65')).json());
  const original = await inspect();
  await page.locator('#generateContours').click();
  await page.waitForFunction(() => state.contours && contourData && !state.busy);
  assert.deepEqual(await inspect(), original, 'cartographic preparation changed the edited DTM');
  const result = await page.evaluate(() => contourData);
  assert.equal(result.parameters.smoothRadius, 1);
  assert.equal(result.parameters.elevationUnit, 'm');
  assert(result.quality.comparedCells > 0);
  assert(Number.isFinite(result.quality.rmseChange));
  assert.match(await page.locator('#contourQuality').textContent(), /RMS change/);
  const downloadPromise = page.waitForEvent('download');
  await page.locator('#saveContourReport').click();
  const reportDownload = await downloadPromise;
  assert.equal(reportDownload.suggestedFilename(), 'contour-report.json');
  const report = JSON.parse(await readFile(await reportDownload.path(), 'utf8'));
  assert.equal(report.parameters.elevationQuantum, .03048);
  assert.equal(report.quality.comparedCells, result.quality.comparedCells);
  assert(report.quality.warnings.some(w => w.includes('not vertical accuracy')));

  await page.locator('[data-tool=flatten]').click();
  await page.locator('#waterLevel').fill('54.123');
  for (const [x,y] of [[45,50], [80,50], [80,88], [45,88]]) {
    const point = await page.evaluate(({x,y}) => {
      const g = geometry(), r = surface.getBoundingClientRect();
      return {x:r.left+g.w/2+(x/state.raster.width-camera.x)*g.dw,
        y:r.top+g.h/2+(y/state.raster.height-camera.y)*g.dh};
    }, {x,y});
    await page.mouse.click(point.x, point.y);
  }
  await page.locator('#applySelection').click();
  await page.waitForFunction(() => !editingPending && state.editing.undo === 1 && !state.busy);
  assert.equal(await page.evaluate(() => state.contours), null, 'terrain edit kept stale contour QA');
  assert.equal(await page.locator('#contourQuality').textContent(), '', 'stale QA remained visible');
  await page.locator('#contourTab').click();
  await page.locator('#generateContours').click();
  await page.waitForFunction(() => !state.busy && contourData?.revision === state.editing.revision);
  assert((await page.evaluate(() => contourData.quality.preservedHydroBlocks)) > 0);

  // Enable auto-refresh and ensure the extended parameter serialization settles.
  await page.locator('#autoContours').check();
  await page.locator('#contourQuantum').fill('0.02');
  await page.locator('#contourQuantum').press('Tab');
  await page.waitForFunction(() => !state.busy && contourData?.parameters.elevationQuantum === .02);
  assert.equal(await page.evaluate(() => state.contours.revision),
    await page.evaluate(() => state.editing.revision));
  const surfaceDownloadPromise = page.waitForEvent('download');
  await page.locator('#saveContourSurface').click();
  const surfaceDownload = await surfaceDownloadPromise;
  assert.equal(surfaceDownload.suggestedFilename(), 'contour-surface.tif');
  await mkdir('.tmp', {recursive:true});
  await surfaceDownload.saveAs('.tmp/cartographic-surface.tif');
  await page.locator('#viewContourSurface').click();
  await page.waitForFunction(() => after !== null);
  await page.screenshot({path:'.tmp/cartographic-preview.png', fullPage:true});
  await page.locator('#autoContours').uncheck();
  for (const [unit, expected] of [['ft', .1], ['US ft', .03048/(1200/3937)], ['native', 0]]) {
    await page.locator('#contourHeightUnit').selectOption(unit);
    await page.locator('#contourPreset').selectOption('cartographic');
    assert.equal(Number(await page.locator('#contourQuantum').inputValue()), expected);
  }
  assert.deepEqual(errors, []);
  assert.equal(await page.locator('#error').isVisible(), false);
  console.log('PASS cartographic UI: unit-aware preset, DTM isolation, quality/report download, water constraints, stale QA invalidation, auto-refresh and TIFF export.');
} finally {
  await browser.close();
}
