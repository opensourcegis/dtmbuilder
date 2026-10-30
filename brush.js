// One gesture selects a swept circular brush and commits once on release. The
// server evaluates all selected cells against the same pre-stroke DTM snapshot.
let brushStroke = null;
function brushActive() { return brushStroke !== null; }

function beginBrushStroke(e, point) {
  if (editingPending || uploading || state.busy || !state.complete) return;
  const radius = Number($('brushRadius').value);
  const threshold = Number($('editThreshold').value);
  if (!Number.isInteger(radius) || radius < 1 || radius > 50 ||
      !Number.isFinite(threshold) || threshold < 0) {
    fail('Enter a brush radius of 1–50 pixels and a nonnegative spike deviation.');
    return;
  }
  clearError();
  actionEpoch++;
  brushStroke = {id:e.pointerId, points:[point], radius, threshold,
    revision:state.editing.revision, sourceVersion:version};
  editingPending = true;
  surface.setPointerCapture(e.pointerId);
  updateGIS(state);
  draw();
}

function appendBrushPoint(e, force = false) {
  if (!brushStroke || e.pointerId !== brushStroke.id) return;
  const point = cellAt(e, true), points = brushStroke.points;
  if (!point) return;
  const last = points.at(-1);
  const distance = Math.hypot(point.x-last.x, point.y-last.y);
  if (distance < 1e-6 || (!force && distance < Math.max(.5, brushStroke.radius/4))) return;
  // Collapse straight runs while retaining bends. The preview and server use
  // the same resulting path; no intermittent mouse events leave coverage gaps.
  if (points.length > 1) {
    const first = points.at(-2), dx = point.x-first.x, dy = point.y-first.y;
    const length = Math.hypot(dx,dy);
    const dot = (last.x-first.x)*(point.x-last.x)+(last.y-first.y)*(point.y-last.y);
    if (length > 0 && dot >= 0 &&
        Math.abs(dx*(last.y-first.y)-dy*(last.x-first.x))/length < .05) {
      points[points.length-1] = point;
      return;
    }
  }
  if (points.length >= 256) {
    cancelBrushStroke();
    fail('Brush stroke has too many turns. Use a shorter stroke. No changes were applied.');
    return;
  }
  points.push(point);
}

function releaseBrushCapture(id) {
  if (surface.hasPointerCapture(id)) surface.releasePointerCapture(id);
}

function cancelBrushStroke() {
  if (!brushStroke) return;
  const id = brushStroke.id;
  brushStroke = null;
  releaseBrushCapture(id);
  editingPending = false;
  updateGIS(state);
  draw();
}

function finishBrushStroke(e) {
  if (!brushStroke || e.pointerId !== brushStroke.id) return;
  appendBrushPoint(e, true);
  if (!brushStroke) return;
  const stroke = brushStroke;
  brushStroke = null;
  releaseBrushCapture(stroke.id);
  editingPending = false;
  if (stroke.sourceVersion !== version) { updateGIS(state); draw(); return; }
  // submitEdit synchronously acquires editingPending before its first await.
  submitEdit({tool:'spike', points:stroke.points, radius:stroke.radius,
    threshold:stroke.threshold, revision:stroke.revision});
}

function drawBrushStroke(ctx, g) {
  if (!brushStroke || !state.raster) return;
  const {points, radius} = brushStroke;
  ctx.save();
  ctx.translate(g.w/2-camera.x*g.dw, g.h/2-camera.y*g.dh);
  ctx.scale(g.dw/state.raster.width, g.dh/state.raster.height);
  ctx.strokeStyle = '#2ca5da55';
  ctx.fillStyle = '#2ca5da55';
  ctx.lineWidth = radius*2;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.beginPath();
  if (points.length === 1) {
    ctx.arc(points[0].x, points[0].y, radius, 0, Math.PI*2);
    ctx.fill();
  } else {
    ctx.moveTo(points[0].x, points[0].y);
    for (const point of points.slice(1)) ctx.lineTo(point.x, point.y);
    ctx.stroke();
  }
  ctx.restore();
}

surface.addEventListener('pointermove', e => {
  if (!brushStroke || e.pointerId !== brushStroke.id) return;
  appendBrushPoint(e);
  draw();
});
surface.addEventListener('pointerup', finishBrushStroke);
for (const name of ['pointercancel', 'lostpointercapture']) {
  surface.addEventListener(name, e => {
    if (brushStroke?.id === e.pointerId) cancelBrushStroke();
  });
}
document.addEventListener('keydown', e => {
  if (e.key === 'Escape' && brushStroke) {
    e.preventDefault();
    cancelBrushStroke();
  }
});
window.addEventListener('blur', cancelBrushStroke);
