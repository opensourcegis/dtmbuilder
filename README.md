# Terrain Studio GIS

Double-click **Terrain Studio Area Filter.exe** or **Start-Terrain-Studio.cmd** for the Windows
desktop application. It has a light interface, grouped parameters, native Windows
Open/Save dialogs, and an embedded terrain viewer. No separate browser or local
TCP server is started. Reopening the desktop EXE brings its existing window forward.
Windows WebView2 is required (already installed on this computer).
Processing and GeoTIFF I/O run in Go without Python, GDAL or npm runtime dependencies.
The original `terrain-go.exe` is an older browser build; `Terrain Studio.exe`,
`Terrain Studio Navigation.exe` and `Terrain Studio GIS.exe` are previous versions. This version can
run alongside them, preserving access to existing unsaved results. Subsequent GIS
launches focus the existing GIS window.

1. Open a single-band elevation GeoTIFF or choose **Try demonstration**.
2. Choose a preset and adjust cleanup and terrain controls.
3. Run processing, slide the comparison divider and inspect the different layers.
4. Save DTM, cleaned DSM, nDSM, flags and a JSON report. Save/load settings
   to repeat a configuration. All jobs start from the original input.

## GIS workspace and manual editing

### Contours and a smoother contour surface

The default preset is **Normal DTM contours · area filter**: averaging factor 1,
no focal/regional smoothing and no line-length suppression. Contours therefore
come from the current DTM directly. Closed contour loops enclosing **less than
50 square metres** are omitted from both map display and CSV line export. A loop
at the threshold or larger is retained (within a small floating-point tolerance).
Open contours, including those ending at the raster edge or NoData, are not
removed by this area criterion. The DTM and contour surface remain unchanged.

**Minimum enclosed area** is adjustable; 0 disables area filtering. Physical-area
filtering requires georeferenced projected coordinates with recognized metre,
international-foot or US-survey-foot units. Foot areas are converted to m²; the
affine determinant accounts for rotation and differing X/Y cell sizes. Angular,
unknown-unit and unreferenced rasters are rejected when a non-zero m² filter is
requested. Reproject those rasters first, or use 0 explicitly; synthetic demo
data has no CRS and needs 0. The interface reports how many small loops were omitted.

**Regional terrain** and **Broad regional terrain** presets generalize the contour
surface across wider neighbourhoods. For projected rasters with recognized metre/
foot units and pixel spacing, Regional targets about 0.5 m cells and Broad targets
about 1 m cells, bounded by the 1–100 averaging factor limit. These targets never
upsample a coarse raster. For unknown/angular units, presets use conservative
pixel-based factors; use explicit settings or reproject before physical-scale work.

The **Regional radius** is measured in the averaged contour grid's cells (0–100).
Three broad mean passes approximate a Gaussian low-pass surface while preserving
NoData. Radius 5 on a 0.5 m grid has an 11×11 (5.5 m wide) window per pass; three
passes have a wider combined influence. Radius 0 disables this operation. This is
cartographic generalization, not ground classification or recovery beneath roofs.

**Minimum line length** suppresses entire short contour polylines, including loops,
in both display and CSV export. Length uses native horizontal map units (metres,
feet, degrees, or unknown map units), or source pixels if unreferenced. It does not
delete terrain cells or connect unrelated lines. Use 0 to retain all contours.
Presets use 20 m / 50 m thresholds for recognized metre/foot CRSs; no fixed physical
threshold is applied to angular or unknown map units. Degrees are not metres.
Filtering can hide real small depressions or landforms; compare with the full-detail
view. Open contours at NoData boundaries stay open.

**View contour surface hillshade** displays the actual generalized surface beneath
its contours, aligned to the source extent. Manual drawing tools return to the
editable DTM layer. Save the separate contour surface to retain the prepared grid.
If contours outline rectangular buildings/blocks, inspect and correct the DTM
first (larger terrain-object windows or polygon interpolation); smoothing or hiding
loops cannot prove that those surface objects have been removed correctly.

Contours automatically generate from the current DTM after extraction, direct
DTM import, or manual edits. Toggle **DTM contours** in the layer dock. The
**Contours** tab controls interval (default 1 raster height unit), base elevation,
index frequency (default every fifth line), block averaging and mean smoothing.
Contour lines and labels share map zoom/pan; comparison mode clips the DTM contours
to the result side. Turn off automatic refresh to regenerate manually.

For **1 m vertical contour intervals**, a **0.5–1 m horizontal DTM grid** is a
reasonable starting trial, not a universal specification. Check vertical accuracy
against independent surveyed elevations and your project's acceptance criteria.
The traditional Class 1 contour benchmark in the
[USACE surveying manual](https://www.publications.usace.army.mil/portals/76/publications/engineermanuals/em_1110-1-1005.pdf)
is vertical RMSE no greater than one-third of the contour interval (about 0.33 m
for 1 m contours). Grid resolution alone cannot establish this accuracy.

Clean/extract the DTM before smoothing ground contours: mean-averaging an uncleaned
DSM can spread building and vegetation heights into ground. For a 0.1 m DTM, try
5×5 block averaging for a 0.5 m contour surface, followed by one 3×3 focal mean
pass. At 0.5 m resolution the 3×3 footprint is 1.5 m. Compare against the unsmoothed
terrain, especially at banks, ridges and embankments. Increasing averaging or
smoothing removes detail and does not improve absolute accuracy by itself.
[Esri's contouring guidance](https://pro.arcgis.com/en/pro-app/3.5/tool-reference/spatial-analyst/how-contouring-works.htm)
also describes preprocessing with focal mean statistics for smoother contours.

All preparation is on a separate **contour surface**: source DSM and edited DTM
stay unchanged. Block averages use complete square blocks; incomplete right/bottom
edge blocks are cropped. Any block containing NoData remains NoData. The focal
mean uses valid neighbours but retains NoData at missing centre cells. Marching
squares skips cells touching NoData, interpolates crossings between cell centres,
resolves saddles consistently, and joins segments into polylines. Contours stop
at raster centre boundaries and NoData gaps. No vector-line smoothing is applied.

Limits: at most 4096 elevation levels and 200,000 traced segments per generation.
Increase interval/averaging/smoothing if limits are reached. Settings use raster
height units, so a 1 m contour interval requires metre elevation values (use an
appropriate converted interval for feet). Averaging factors are integer 1–100;
the smoothing radius is 0–5 (none to 11×11). The app does not invent finer terrain
resolution by upsampling coarse input.

Save the contour surface as a georeferenced float32 TIFF, or the lines as CSV
with elevation, index flag and WKT LINESTRING geometry. CSV coordinates use the
source raster CRS; in QGIS choose WKT geometry and assign the source raster's CRS.
Unreferenced demonstration data exports source pixel coordinates, explicitly
identified in the CSV. CSV is not an EPSG:4326 GeoJSON export.

The left dock selects raster layers and shows quality counts. The centre contains
the hillshade canvas and navigation tools. The right dock has **Edit terrain** and
**Process DSM** tabs. The status bar shows the source cell under the cursor;
**Identify** displays its actual elevations and map coordinates when georeferenced.
Enable **Compare with input** for the synchronized before/after slider.

Use **Open DTM** to edit existing terrain without running extraction. Alternatively,
load a DSM and run extraction, or explicitly choose **Use input as editable DTM**.
The source raster and cleaned DSM remain unchanged during manual editing.

* **Repair spikes:** click a circular area (radius 1–50 source pixels). Each valid
  selected cell is compared with its surrounding 5×5 median (excluding itself).
  Replace deviations above the specified absolute threshold. This repairs both
  high and low spikes. A threshold of zero repairs all differing selected cells.
* **Hydroflatten:** click polygon vertices, enter a known water-surface elevation,
  then **Apply selection**. All selected valid cells become exactly that height.
  Identify a source cell and choose **Use identified DTM elevation** to copy a level.
  Intended for lakes/reservoirs; it does not grade rivers or enforce drainage flow.
* **Smooth:** draw a polygon and Apply to replace selected valid cells with the
  mean of their valid eight neighbours, using the pre-edit surface for every cell.
* **Interpolate:** draw a polygon and Apply to rebuild it using inverse-distance-
  squared weighting from valid cells immediately outside its boundary (up to 128
  deterministically sampled boundary cells). At least three boundary cells are
  required. This is patch reconstruction, not constrained breakline interpolation.

Polygon selections are drawn in source-cell coordinates. Double-click/Enter closes
the outline; **Apply** commits it. Escape/Clear removes only the selection. Use a
middle-button drag to pan while an edit tool is active. Edits are limited to 250,000
source cells per action and a 2-million-cell selection bounding box; self-crossing
polygons are rejected. Preview imagery is downsampled, so zooming enlarges the
preview while source-cell calculations remain at the input resolution.

NoData is skipped by default. **Include NoData** allows hydroflatten/interpolation
to fill it deliberately; smoothing and spike repair always skip NoData. Edited
DTM heights may exceed the cleaned DSM for user-specified water levels. nDSM is
recomputed as max(0, cleaned DSM − edited DTM); it remains NoData where the cleaned
DSM itself has no height. Edited DTM exports retain the original geometry and CRS.

**Undo/Redo** (Ctrl+Z / Ctrl+Y) restores terrain and quality flags, including NaN
NoData values. History retains up to 50 actions / approximately 32 MiB of cell
deltas. A new edit clears redo. Loading another raster or rerunning extraction
resets history. **Save DTM** saves the edited elevation raster. The **Edit / run
report** records operation types, source-cell polygon/brush coordinates, thresholds,
water levels, changed-cell counts, timestamps and undo/redo events. It is an audit
log, not a reloadable project. Sessions/history are held in memory; save the DTM
and report before exiting. Desktop close warns about unsaved manual edits.

Navigate the DEM viewer with the mouse wheel to zoom around the cursor, and
left-click-drag (or middle-drag) to pan. Both comparison layers stay aligned.
Use +/− buttons to zoom, **Fit / reset** or double-click to show the whole raster.
When the viewer has focus, +/− zoom, arrow keys pan and Home/0 reset the view.
Zoom is 25–3200% relative to the fitted view. Navigation is preserved when
switching result layers or rerunning processing, and resets when loading a new
input. Zoom enlarges the downsampled preview; source data and export resolution
are unchanged.

## Controls and method

Neighbourhood median filtering excludes the central sample and requires at least
five valid neighbours. Separate low/high thresholds replace samples outside the
larger of the absolute height threshold and the robust MAD cutoff. Spike filtering
also requires a small fraction of neighbours close to the central height. Low/high
rules take precedence over spike rules. Replacements use the neighbourhood median.

Terrain estimation progressively applies square morphological openings at doubling
radii up to the configured maximum. Samples are lowered when their height above
the opening exceeds `groundThreshold + slope * radius`. Optional smoothing averages
3x3 neighbourhoods and caps elevations at the cleaned DSM. This is an independent
morphological estimator, not a port of Bulldozer's cloth simulation. It has not
been validated as more accurate than Bulldozer on surveyed terrain.

Heights and height thresholds use the source raster's elevation units. Radius is
in pixels. To target a 20 m wide object at 0.1 m resolution, try an opening radius
of approximately 100 pixels (window width 201 pixels). Slope allowance is elevation
units per pixel of opening radius, not degrees or percent. Presets are starting
points, not terrain classifications. Large windows can remove hills and embankments.

Set low/high/spike thresholds individually to zero to disable them. Set the opening
radius and smoothing to zero for cleanup only. Gap filling is bounded by the configured
number of four-neighbour propagation steps. With **Keep filled cells in exported rasters**
unchecked, original NoData is restored in exports. With it checked, interpolated
cells are kept in DTM, cleaned DSM and nDSM. Cells beyond the maximum fill distance
stay NoData. A distance of zero disables all filling. Output counts show filled
and remaining NoData cells. Filled heights are estimates from neighbouring elevations.
nDSM = max(0, cleaned DSM - DTM).
Flags are bitwise combined: 1 low repair, 2 high repair, 4 spike repair,
8 height above estimated ground exceeds ground tolerance, 16 original NoData,
32 interpolated cell retained in exports, 64 manually edited, 128 hydroflattened.
Filled cells combine bits 16 and 32. Other flags may also combine.
These are raster cell flags, not LAS point classifications.

## Input/output limits

Classic TIFF, single band, integer 8/16/32/64 or float32/64, little/big endian,
strips or tiles; uncompressed, Deflate or TIFF LZW; predictors 1/2/3.
RGB and multiband images are rejected. Supports a maximum of 16 million cells
and 512 MiB input files. Processing is in memory, single job at a time; large
rasters should be cropped/resampled first. BigTIFF, JPEG, internal validity masks,
external .msk files, overviews, LAS/LAZ and user ground-anchor masks are not supported.
Only GDAL_NODATA and nonfinite values are treated as missing samples.

Outputs are uncompressed float32 TIFF with NaN NoData; model scale/tiepoint/transform
and GeoKey metadata are copied. Input geometry, resolution and CRS are retained.
Additional source metadata, overviews and raster statistics are not copied.
Demo data intentionally has no CRS. All elevation layers (input DSM, cleaned DSM,
DTM and nDSM) display as grayscale hillshade with the same northwest light
(azimuth 315 degrees, altitude 45 degrees). NoData is transparent; flags retain
their category colours. Previews are downsampled, and elevation exports remain
float32 elevation data. The renderer uses central differences with one-sided
derivatives at edges/NoData boundaries and the GeoTIFF pixel spacing/affine basis.
Projected rasters assume matching elevation and horizontal units. Geographic
rasters use an approximate local metres-per-degree scale and assume metre heights;
for high-latitude or unusual-unit datasets, reproject before comparing relief.

The desktop window communicates with Go in-process. Optional browser mode binds
only to loopback and uses a session token. Inputs and results live in memory;
save results before closing the application.

## Build and CLI

Requires Go 1.24 or newer, Wails v2 and Windows WebView2 for the desktop build.

```powershell
go test ./...
go build -tags production -ldflags "-H windowsgui" -o "Terrain Studio Area Filter.exe" .
& ".\Terrain Studio Area Filter.exe"
go run . -input "C:\data\elevation_dsm.tif" -output "C:\data\terrain-results" -config terrain-settings.json
```

CLI output is a directory. Existing raster output names are refused to avoid
overwriting them; use a new directory for each run. `-config` is optional.
Optional browser mode: `go run . -browser -port 8788`; `-open=false` suppresses
automatic browser launch. Sources: [CNES Bulldozer](https://github.com/CNES/bulldozer),
[TIFF 6.0 specification](https://download.osgeo.org/geotiff/spec/tiff6.pdf).
