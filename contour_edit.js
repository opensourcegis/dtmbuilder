let contourDrag=null,pinListKey='';
function contourDragActive(){return contourDrag!==null}
function beginContourDrag(e,p){
 if(!contourData||state.contourBusy||contourData.revision!==state.editing?.revision||contourData.generation!==state.contours?.generation){fail('Wait for current contours, then drag a contour handle.');return}
 if(contourData.parameters.averageFactor!==1){fail('Set block average factor to 1 before moving/pinning a contour.');return}
 const radius=Number($('contourPinRadius').value);
 if(!Number.isInteger(radius)||radius<2||radius>50){fail('Choose a constraint radius of 2–50 source pixels.');return}
 const g=geometry(),sx=g.dw/state.raster.width,sy=g.dh/state.raster.height;
 let best=null,distance=12;
 for(const item of contourPaths){
  if(p.x<item.minx-12/sx||p.x>item.maxx+12/sx||p.y<item.miny-12/sy||p.y>item.maxy+12/sy)continue;
  const line=item.line;
  for(let i=0;i<line.points.length;i++){const point=line.points[i],d=Math.hypot((point.x-p.x)*sx,(point.y-p.y)*sy);if(d<distance){distance=d;best={line:line.id,vertex:i,point}}}
 }
 if(!best){$('selectionInfo').textContent='Click within 12 screen pixels of a contour vertex.';return}
 clearError();actionEpoch++;editingPending=true;
 contourDrag={...best,id:e.pointerId,target:{...best.point},radius,revision:state.editing.revision,generation:contourData.generation};
 surface.setPointerCapture(e.pointerId);updateGIS(state);draw();
}
function moveContourDrag(e){
 if(!contourDrag||e.pointerId!==contourDrag.id)return;
 const p=cellAt(e,true);if(!p)return;
 contourDrag.target={x:Math.max(.5,Math.min(state.raster.width-.5,p.x)),y:Math.max(.5,Math.min(state.raster.height-.5,p.y))};draw();
}
function cancelContourDrag(){
 if(!contourDrag)return;const id=contourDrag.id;contourDrag=null;
 if(surface.hasPointerCapture(id))surface.releasePointerCapture(id);
 editingPending=false;updateGIS(state);draw();
}
async function finishContourDrag(e){
 if(!contourDrag||e.pointerId!==contourDrag.id)return;moveContourDrag(e);
 const d=contourDrag;contourDrag=null;if(surface.hasPointerCapture(d.id))surface.releasePointerCapture(d.id);
 try{update(await(await api('contours/pin',{method:'POST',body:JSON.stringify({line:d.line,vertex:d.vertex,x:d.target.x,y:d.target.y,radius:d.radius,revision:d.revision,generation:d.generation})})).json());await refreshImages()}
 catch(e){fail(e)}finally{editingPending=false;updateGIS(state)}
}
function drawContourPins(ctx,g){
 if(!state.raster)return;const project=p=>({x:g.w/2+(p.x/state.raster.width-camera.x)*g.dw,y:g.h/2+(p.y/state.raster.height-camera.y)*g.dh});
 ctx.save();ctx.strokeStyle='#9c348c';ctx.fillStyle='#fff';ctx.lineWidth=2;
 for(const pin of state.constraints||[]){const p=project(pin);ctx.beginPath();ctx.arc(p.x,p.y,5,0,Math.PI*2);ctx.fill();ctx.stroke()}
 if(contourDrag){
  const line=contourData?.lines[contourDrag.line];if(line){const i=contourDrag.vertex;ctx.beginPath();
   for(let j=Math.max(0,i-1);j<=Math.min(line.points.length-1,i+1);j++){const p=project(j===i?contourDrag.target:line.points[j]);if(j===Math.max(0,i-1))ctx.moveTo(p.x,p.y);else ctx.lineTo(p.x,p.y)}ctx.stroke();
  }
  const p=project(contourDrag.target);ctx.beginPath();ctx.arc(p.x,p.y,6,0,Math.PI*2);ctx.fill();ctx.stroke();
 }
 ctx.restore();
}
function updateContourPins(s){
 const pins=s.constraints||[],key=JSON.stringify(pins)+!!editingPending+!!s.busy;
 if(key===pinListKey)return;pinListKey=key;
 const items=pins.map(pin=>{const button=document.createElement('button');button.className='full';button.textContent=`Remove pin #${pin.id} · height ${pin.elevation.toPrecision(6)}`;button.disabled=editingPending||s.busy;button.onclick=()=>removeContourPin(pin.id);return button});
 if(!items.length)$('constraintList').textContent='No contour elevation pins.';else $('constraintList').replaceChildren(...items);
}
async function removeContourPin(id){
 if(editingPending||state.busy)return;editingPending=true;actionEpoch++;clearError();updateGIS(state);
 try{update(await(await api('contours/unpin',{method:'POST',body:JSON.stringify({id,revision:state.editing.revision})})).json());await refreshImages()}
 catch(e){fail(e)}finally{editingPending=false;updateGIS(state)}
}
surface.addEventListener('pointermove',moveContourDrag);surface.addEventListener('pointerup',finishContourDrag);
for(const name of ['pointercancel','lostpointercapture'])surface.addEventListener(name,e=>{if(contourDrag?.id===e.pointerId)cancelContourDrag()});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&contourDrag){e.preventDefault();cancelContourDrag()}});
window.addEventListener('blur',cancelContourDrag);
