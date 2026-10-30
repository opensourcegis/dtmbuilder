// Drawn vector constraints share terrain revisions, history and contour refresh.
let featureRings=[],terrainFeatureData=[],featurePaths=[],featureFetchKey='',featureListKey='',featureFetchToken=0,featureSourceVersion=-1;
function isTerrainFeatureTool(){return ['breakline','waterbody'].includes(activeTool)}
function resetFeatureDraft(){featureRings=[]}
function featureRingsForApply(){return [...featureRings,...(polygon.length?[polygon.map(p=>({...p}))]:[])]}
function updateFeatureSelection(){
 const rings=featureRingsForApply(),vertices=rings.reduce((n,r)=>n+r.length,0),valid=activeTool==='breakline'?polygon.length>=2:rings.length>0&&rings.every(r=>r.length>=3);
 $('selectionInfo').textContent=vertices?`${vertices} vertices${activeTool==='waterbody'?` · ${Math.max(0,rings.length-1)} island rings`:''} · ${polygonClosed?'ready':'drawing'}`:'No selection';
 $('applySelection').disabled=editingPending||uploading||!state.complete||state.busy||!valid;
 $('addIslandRing').disabled=editingPending||uploading||state.busy||activeTool!=='waterbody'||polygon.length<3||featureRings.length>=16;
}
$('addIslandRing').onclick=()=>{
 if(editingPending||uploading||state.busy||activeTool!=='waterbody'||polygon.length<3)return;
 featureRings.push(polygon.map(p=>({...p})));polygon=[];polygonClosed=false;updateSelection();draw();
};
$('breaklineMode').onchange=()=>{$('gradeOptions').hidden=$('breaklineMode').value==='preserve'};
function useConstrainedSurfaceSettings(){
 $('contourAverage').value=1;$('contourSmooth').value=0;$('regionalRadius').value=0;$('contourMinArea').value=0;$('contourMinLength').value=0;
 if(Number($('edgePasses').value)===0){$('edgePasses').value=4;$('edgeThreshold').value=Math.max(.0001,Number($('contourInterval').value)*.5)}
 if(Number($('maxSurfaceChange').value)===0)$('maxSurfaceChange').value=Math.max(.0001,Number($('contourInterval').value)*.2);
 $('contourPreset').value='constrained';contourAttempt='';
}
async function applyDrawnTerrainFeature(){
 if(editingPending||state.busy||uploading||!state.complete)return;
 let q={kind:activeTool,revision:state.editing.revision,fillNoData:$('editFillNoData').checked};
 if(activeTool==='waterbody'){
  const rings=featureRingsForApply();if(!rings.length||rings.some(r=>r.length<3)){fail('Draw an outer water boundary and complete any island rings.');return}
  if($('waterLevel').value.trim()===''||!Number.isFinite(Number($('waterLevel').value))){fail('Enter the known water surface elevation.');return}
  Object.assign(q,{points:rings[0],holes:rings.slice(1),elevation:Number($('waterLevel').value)});
 }else{
  if(polygon.length<2)return;
  const mode=$('breaklineMode').value;
  if(mode!=='preserve'&&[$('breaklineStart'),$('breaklineEnd')].some(el=>el.value.trim()===''||!Number.isFinite(Number(el.value)))){fail('Enter both endpoint heights for a graded breakline.');return}
  Object.assign(q,{points:polygon.map(p=>({...p})),mode,radius:Number($('breaklineRadius').value),elevation:mode==='preserve'?0:Number($('breaklineStart').value),endElevation:mode==='preserve'?0:Number($('breaklineEnd').value)});
 }
 editingPending=true;actionEpoch++;clearError();updateGIS(state);
 try{
  const next=await(await api('terrain-features',{method:'POST',body:JSON.stringify(q)})).json();
  useConstrainedSurfaceSettings();update(next);clearSelection();$('layer').value='dtm';$('compareMode').checked=false;compareChanged();await refreshImages();
 }catch(e){fail(e)}finally{editingPending=false;updateGIS(state);draw()}
}
async function removeDrawnTerrainFeature(id){
 if(editingPending||state.busy||uploading)return;editingPending=true;actionEpoch++;clearError();updateGIS(state);
 try{update(await(await api('terrain-features/remove',{method:'POST',body:JSON.stringify({id,revision:state.editing.revision})})).json());await refreshImages()}
 catch(e){fail(e)}finally{editingPending=false;updateGIS(state);draw()}
}
function updateTerrainFeatures(s){
 const summaries=s.terrainFeatures||[],key=JSON.stringify(summaries)+!!editingPending+!!s.busy+!!uploading;
 if(key!==featureListKey){
  featureListKey=key;
  const items=summaries.map(f=>{const row=document.createElement('div'),text=document.createElement('p'),button=document.createElement('button');
   text.textContent=`#${f.id} ${f.kind} · ${f.mode} · ${f.cells.toLocaleString()} protected cells`;
   button.textContent=`Remove #${f.id}`;button.disabled=editingPending||s.busy||uploading;button.onclick=()=>removeDrawnTerrainFeature(f.id);row.append(text,button);return row});
  if(items.length)$('terrainFeatureList').replaceChildren(...items);else $('terrainFeatureList').textContent='No protected breaklines or waterbodies.';
 }
 if(featureSourceVersion!==version){featureSourceVersion=version;featureFetchKey='';terrainFeatureData=[];featurePaths=[];featureFetchToken++;resetFeatureDraft()}
 const fetchKey=version+':'+s.featureGeneration;
 if(s.ready&&fetchKey!==featureFetchKey){
  featureFetchKey=fetchKey;const token=++featureFetchToken;
  api('terrain-features').then(r=>r.json()).then(result=>{
   if(token!==featureFetchToken||result.generation!==state.featureGeneration)return;
   terrainFeatureData=result.features||[];
   featurePaths=terrainFeatureData.map(f=>{const path=new Path2D();for(const ring of [f.points,...(f.holes||[])]){ring.forEach((p,i)=>{if(i)path.lineTo(p.x,p.y);else path.moveTo(p.x,p.y)});if(f.kind==='waterbody')path.closePath()}return {feature:f,path}});draw();
  }).catch(e=>{if(token===featureFetchToken){featureFetchKey='';fail(e)}});
 }
}
function drawTerrainFeatures(ctx,g){
 if(!state.raster)return;
 ctx.save();if($('compareMode').checked){const split=g.w*Number($('split').value)/100;ctx.beginPath();ctx.rect(split,0,g.w-split,g.h);ctx.clip()}
 const scale=g.dw/state.raster.width;ctx.translate(g.w/2-camera.x*g.dw,g.h/2-camera.y*g.dh);ctx.scale(scale,g.dh/state.raster.height);
 for(const item of featurePaths){const f=item.feature;ctx.lineWidth=1.5/scale;ctx.strokeStyle=f.kind==='waterbody'?'#157ba2':'#ae467d';
  if(f.kind==='waterbody'){ctx.fillStyle='#198bbc15';ctx.fill(item.path,'evenodd')}else{ctx.save();ctx.globalAlpha=.12;ctx.lineWidth=f.radius*2;ctx.stroke(item.path);ctx.restore()}
  ctx.stroke(item.path);
 }
 if(activeTool==='waterbody'&&featureRings.length){ctx.strokeStyle='#1688bc';ctx.lineWidth=1.5/scale;ctx.setLineDash([5/scale,3/scale]);for(const ring of featureRings){ctx.beginPath();ring.forEach((p,i)=>{if(i)ctx.lineTo(p.x,p.y);else ctx.moveTo(p.x,p.y)});ctx.closePath();ctx.stroke()}}
 ctx.restore();
}
