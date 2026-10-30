let contourData=null,contourAttempt='',contourFetchKey='',contourStarting=false,contourFetchToken=0;
let contourTimer=null,contourPaths=[],contourFetchAbort=null,contourSource=-1;
function stopContourRefresh(){clearTimeout(contourTimer?.id);contourTimer=null;contourAttempt=state.editing?.revision+':'+JSON.stringify(contourParams())}
$('cancelContours').onclick=cancelJobs;
function contourParams(){return {interval:Number($('contourInterval').value),base:Number($('contourBase').value),indexEvery:Number($('contourIndex').value),averageFactor:Number($('contourAverage').value),smoothRadius:Number($('contourSmooth').value),regionalRadius:Number($('regionalRadius').value),minLength:Number($('contourMinLength').value),minArea:Number($('contourMinArea').value),elevationQuantum:Number($('contourQuantum').value),elevationUnit:$('contourHeightUnit').value,preserveHydro:$('preserveHydro').checked,edgePasses:Number($('edgePasses').value),edgeThreshold:Number($('edgeThreshold').value),maxSurfaceChange:Number($('maxSurfaceChange').value)}}
function openContourTab(){$('editorPanel').hidden=true;$('processingPanel').hidden=true;$('contourPanel').hidden=false;$('editTab').classList.remove('active');$('processTab').classList.remove('active');$('contourTab').classList.add('active')}
$('contourTab').onclick=openContourTab;
async function makeContours(){
 if(state.busy||!state.complete||contourStarting||editingPending||uploading)return;
 const p=contourParams();if(!Number.isFinite(p.interval)||p.interval<=0){fail('Enter a positive contour interval in source height units.');return}
 contourAttempt=state.editing.revision+':'+JSON.stringify(p);contourStarting=true;actionEpoch++;clearError();
 try{await api('contours/generate',{method:'POST',body:JSON.stringify(p)});state.contourBusy=true;state.message='Refreshing contours; terrain editing remains available';update(state);await poll()}
 catch(e){fail(e)}finally{contourStarting=false;updateContours(state)}
}
$('generateContours').onclick=()=>{clearTimeout(contourTimer?.id);contourAttempt='';makeContours()};
$('showContours').onchange=()=>{draw();if($('showContours').checked&&!contourData)makeContours()};
$('autoContours').onchange=()=>{clearTimeout(contourTimer?.id);contourAttempt='';updateContours(state)};
const contourControls=['contourInterval','contourBase','contourIndex','contourAverage','contourSmooth','regionalRadius','contourMinLength','contourMinArea','contourQuantum','contourHeightUnit','preserveHydro','edgePasses','edgeThreshold','maxSurfaceChange'];
for(const id of contourControls)$(id).onchange=()=>{if(id==='contourHeightUnit'&&$('contourPreset').value==='cartographic'){$('contourQuantum').value=cartographicQuantum()}else $('contourPreset').value='custom';contourAttempt='';updateContours(state)};
function cartographicQuantum(){const unit=$('contourHeightUnit').value;return unit==='m'?.03048:unit==='ft'?.1:unit==='US ft'?.03048/(1200/3937):0}
function updateContours(s){
 const hardFeatures=(s.terrainFeatures||[]).length>0;
 if(hardFeatures&&(Number($('contourAverage').value)!==1||Number($('contourSmooth').value)!==0||Number($('regionalRadius').value)!==0))useConstrainedSurfaceSettings();
 for(const option of $('contourPreset').options)option.disabled=hardFeatures&&!['constrained','custom'].includes(option.value);
 if(s.raster&&contourSource!==version){
  contourSource=version;contourFetchAbort?.abort();contourFetchToken++;contourFetchKey='';contourAttempt='';
  clearTimeout(contourTimer?.id);contourTimer=null;contourData=null;contourPaths=[];draw();
  if($('contourPreset').value==='normal')$('contourMinArea').value=s.raster.areaFilterAvailable?50:0;
 }
 $('areaFilterHint').textContent=s.raster&&!s.raster.areaFilterAvailable?'Square-metre filtering requires recognized projected metre/foot coordinates. The Normal preset keeps all loops for this raster.':'Closed loops below the chosen minimum area are omitted from display and export. Open lines remain. Set 0 to disable.';
 const blocked=s.busy||!s.complete||editingPending||contourStarting||uploading;
 const exportBlocked=blocked||s.contourBusy||!s.contours||s.contours.revision!==s.editing?.revision;
 $('generateContours').disabled=blocked;$('cancelContours').disabled=!s.contourBusy||uploading;
 for(const id of ['saveContours','saveContourSurface','saveContourReport','saveGpkg'])$(id).disabled=exportBlocked;
 for(const id of contourControls)$(id).disabled=!!s.busy||(hardFeatures&&['contourAverage','contourSmooth','regionalRadius'].includes(id));
 $('contourPreset').disabled=!!s.busy;$('viewContourSurface').disabled=exportBlocked;
 $('lengthUnits').textContent=s.raster?.horizontalUnit||'map units';
 if(typeof updateContourPins==='function')updateContourPins(s);
 const rev=s.editing?.revision;
 if(!s.complete){contourFetchAbort?.abort();clearTimeout(contourTimer?.id);contourTimer=null;contourData=null;contourPaths=[];contourFetchToken++;contourFetchKey='';contourAttempt='';$('contourInfo').textContent='Create/open a DTM to generate contours.';$('contourQuality').textContent='';$('contourWarnings').textContent='';return}
 if(!s.contours){$('contourInfo').textContent=s.contourBusy?'Refreshing contours in the background. Continue editing the DTM.':'DTM ready. Generate contours to inspect the current surface.';$('contourQuality').textContent='';$('contourWarnings').textContent=''}
 if(s.contours){
  const c=s.contours,q=c.quality,t=c.performance;
  $('contourQuality').textContent=q?`Surface vs edited DTM: RMS change ${q.rmseChange.toPrecision(4)}, maximum change ${q.maxAbsChange.toPrecision(4)} source height units · ${q.preservedHydroBlocks} water cells/blocks protected · ${q.protectedFeatureCells||0} feature samples (${q.featureSampleMismatches||0} mismatches).`:'';
  $('contourWarnings').textContent=(q?.warnings||[]).join('\n\n');
  $('contourIntervalLabel').textContent=c.parameters.interval+' z';
  $('contourInfo').textContent=`${c.lines.toLocaleString()} lines · ${c.segments.toLocaleString()} segments · interval ${c.parameters.interval} height units · ${c.removedSmallLoops||0} small loops omitted.`+(t?` ${t.mode}: ${t.tilesUpdated} tiles refreshed, ${t.tilesReused} reused · ${Math.round(t.totalMilliseconds)} ms.`:'');
  const key=c.generation+':'+c.revision+':'+JSON.stringify(c.parameters);
  if(key!==contourFetchKey){contourFetchKey=key;const token=++contourFetchToken;contourFetchAbort?.abort();contourFetchAbort=new AbortController();
   api('contours',{signal:contourFetchAbort.signal}).then(r=>r.json()).then(async c=>{
    if(!contourFetchCurrent(c,token))return;
    const paths=await cacheContourPaths(c,token);if(!paths||!contourFetchCurrent(c,token))return;
    contourData=c;contourPaths=paths;
    if($('layer').value==='contour-surface')refreshImages().catch(fail);else draw();
   }).catch(e=>{if(token!==contourFetchToken||e.name==='AbortError')return;contourFetchKey='';if(!state.contourBusy&&state.contours?.generation===c.generation)fail(e)})
  }
 }
 const desired=contourParams(),wanted=rev+':'+JSON.stringify(desired),current=s.contours&&s.contours.revision===rev&&JSON.stringify(s.contours.parameters)===JSON.stringify(desired);
 if(blocked||current||!$('autoContours').checked){clearTimeout(contourTimer?.id);contourTimer=null}
 if($('autoContours').checked&&!blocked&&!current&&contourAttempt!==wanted){
  if(contourTimer?.key!==wanted){clearTimeout(contourTimer?.id);const id=setTimeout(()=>{contourTimer=null;makeContours()},120);contourTimer={id,key:wanted}}
 }
}
function contourFetchCurrent(c,token){return token===contourFetchToken&&c.revision===state.editing?.revision&&c.generation===state.contours?.generation}
async function cacheContourPaths(c,token){
 const paths=[];let checkpoint=performance.now(),visited=0;
 for(const line of c.lines){
  const path=new Path2D();let minx=Infinity,miny=Infinity,maxx=-Infinity,maxy=-Infinity,block=null;const blocks=[];
  for(let i=0;i<line.points.length;i++){
   const p=line.points[i];if(i)path.lineTo(p.x,p.y);else path.moveTo(p.x,p.y);
   minx=Math.min(minx,p.x);maxx=Math.max(maxx,p.x);miny=Math.min(miny,p.y);maxy=Math.max(maxy,p.y);
   if(i%128===0){block={start:i,end:i,minx:p.x,miny:p.y,maxx:p.x,maxy:p.y};blocks.push(block)}
   block.end=i+1;block.minx=Math.min(block.minx,p.x);block.maxx=Math.max(block.maxx,p.x);block.miny=Math.min(block.miny,p.y);block.maxy=Math.max(block.maxy,p.y);
   if(++visited%4096===0&&performance.now()-checkpoint>8){await new Promise(resolve=>setTimeout(resolve,0));if(!contourFetchCurrent(c,token))return null;checkpoint=performance.now()}
  }
  paths.push({line,path,minx,miny,maxx,maxy,blocks});
 }
 return paths;
}
function drawContours(ctx,g){
 if(!$('showContours').checked||!contourData||!state.raster)return;
 const sx=g.dw/state.raster.width,sy=g.dh/state.raster.height,x0=g.w/2-camera.x*g.dw,y0=g.h/2-camera.y*g.dh;
 const bounds={x0:-x0/sx,y0:-y0/sy,x1:(g.w-x0)/sx,y1:(g.h-y0)/sy};
 ctx.save();if($('compareMode').checked){const split=g.w*Number($('split').value)/100;ctx.beginPath();ctx.rect(split,0,g.w-split,g.h);ctx.clip()}
 if(contourData.revision!==state.editing?.revision)ctx.globalAlpha=.3;
 ctx.save();ctx.translate(x0,y0);ctx.scale(sx,sy);const labels=[];
 for(const item of contourPaths){
  if(item.maxx<bounds.x0||item.minx>bounds.x1||item.maxy<bounds.y0||item.miny>bounds.y1)continue;
  const line=item.line;ctx.strokeStyle=line.index?'#744923':'#9b763f';ctx.lineWidth=(line.index?1.3:.7)/sx;ctx.stroke(item.path);
  if(line.index&&labels.length<35){
   for(let i=0;i<line.points.length;i+=Math.max(1,Math.floor(line.points.length/80))){
    const p=line.points[i],x=x0+p.x*sx,y=y0+p.y*sy;
    if(x>35&&x<g.w-35&&y>25&&y<g.h-25&&!labels.some(a=>Math.hypot(a.x-x,a.y-y)<80)){labels.push({x,y,text:Number(line.elevation.toFixed(5)).toString()});break}
   }
  }
 }
 ctx.restore();ctx.font='10px Segoe UI';ctx.textAlign='center';ctx.textBaseline='middle';
 for(const p of labels){const width=ctx.measureText(p.text).width+6;ctx.fillStyle='#ffffffe8';ctx.fillRect(p.x-width/2,p.y-7,width,14);ctx.fillStyle='#694923';ctx.fillText(p.text,p.x,p.y)}ctx.restore();
}
async function saveContourOutput(surfaceOutput){try{if(desktop){const path=await window.go.main.Desktop.ExportContours(surfaceOutput);if(path)$('dirty').textContent='Saved: '+path}else{await download(surfaceOutput?'contours/surface':'contours/export',surfaceOutput?'contour-surface.tif':'contours.csv')}}catch(e){fail(e)}}
$('saveContours').onclick=()=>saveContourOutput(false);$('saveContourSurface').onclick=()=>saveContourOutput(true);
$('saveGpkg').onclick=async()=>{try{if(desktop){const path=await window.go.main.Desktop.ExportContourGPKG();if(path)$('dirty').textContent='Saved: '+path}else await download('contours/gpkg','contours.gpkg')}catch(e){fail(e)}};
$('saveContourReport').onclick=async()=>{try{if(desktop){const path=await window.go.main.Desktop.ExportContourReport();if(path)$('dirty').textContent='Saved: '+path}else await download('contours/report','contour-report.json')}catch(e){fail(e)}};
$('contourPreset').onchange=()=>{const mode=$('contourPreset').value;if(mode==='custom')return;$('edgePasses').value=0;$('edgeThreshold').value=0;$('maxSurfaceChange').value=0;if(mode==='constrained'){useConstrainedSurfaceSettings();$('contourMinArea').value=0;$('contourMinLength').value=0;$('contourQuantum').value=0;$('preserveHydro').checked=true;updateContours(state);return}if(mode==='cartographic'){$('contourAverage').value=1;$('contourSmooth').value=1;$('regionalRadius').value=0;$('contourMinLength').value=0;$('contourMinArea').value=0;$('contourQuantum').value=cartographicQuantum();$('preserveHydro').checked=true;contourAttempt='';updateContours(state);return}$('contourQuantum').value=0;const unit=state.raster?.horizontalUnit||'px',pixel=state.raster?.pixelSize||0,limit=Math.max(1,Math.min(100,Math.floor((state.raster?.width||256)/2),Math.floor((state.raster?.height||256)/2)));let factor=1,radius=0,length=0;
if(!['local','normal'].includes(mode)){const broad=mode==='broad';let metresPerUnit=unit==='m'?1:unit==='ft'?.3048:unit==='US ft'?1200/3937:0;factor=metresPerUnit&&pixel>0?Math.round((broad?1:.5)/(pixel*metresPerUnit)):(broad?4:2);factor=Math.max(1,Math.min(limit,factor));radius=broad?10:5;length=metresPerUnit?(broad?50:20)/metresPerUnit:unit==='px'?(broad?60:30):0}
$('contourAverage').value=factor;$('contourSmooth').value=mode==='local'?1:0;$('contourMinArea').value=mode==='normal'&&(!state.raster||state.raster.areaFilterAvailable)?50:0;$('regionalRadius').value=radius;$('contourMinLength').value=Number(length.toFixed(3));contourAttempt='';updateContours(state)};
$('viewContourSurface').onclick=()=>{$('layer').value='contour-surface';$('layer').onchange();updateGIS(state)};


updateContours(state);
