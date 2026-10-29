# Terrain Studio GIS

Build with **Build-Terrain-Studio.ps1**, then double-click **dist/Terrain-Studio.exe**
or **Start-Terrain-Studio.cmd** for the Windows
desktop application. It has a light interface, grouped parameters, native Windows
Open/Save dialogs, and an embedded terrain viewer. No separate browser or local
TCP server is started. Reopening the desktop EXE brings its existing window forward.
Windows WebView2 is required on the computer running the desktop application.
Processing and GeoTIFF I/O run in Go without Python, GDAL or npm runtime dependencies.
The source was imported from the supplied terrain-go archive. Compiled builds,
screenshots, logs and process-ID files are generated artifacts and are not checked
in. GitHub Actions tests Linux and Windows and publishes downloadable build
artifacts for each successful workflow run. Local outputs go in `dist/`.

1. Open a single-band elevation GeoTIFF or choose **Try demonstration**.
2. Choose a preset and adjust cleanup and terrain controls.
3. Run processing, slide the comparison divider and inspect the different layers.
4. Save DTM, cleaned DSM, nDSM, flags and a JSON report. Save/load settings
   to repeat a configuration. All jobs start from the original input.


## High-resolution DTM: protected features and edge filtering

[Research notes and paper links](docs/contour-research.md) explain the methods,
access limits and validation. The new filter adapts Perona–Malik diffusion to
DTM elevations; it does not reproduce the Sun/Lindsay normal-field method or PAEK.

1. Open the DTM, declare source height units and choose the contour interval.
   In **Contours**, select **High-resolution DTM · constrained edge filter**.
   It keeps source resolution, disables size filters and starts with four edge
   diffusion passes, a contrast of half the interval and a surface-change cap of
   one fifth of the interval. These are adjustable starting values, not accuracy
   criteria. Compare banks, ridges, small hills and drainage against the DTM.
2. Choose **Breakline** and click two or more vertices, then **Apply selection**.
   **Preserve current DTM heights** locks the existing terrain in a 1–5 pixel
   radius corridor. **Grade** uses entered endpoint heights; **Downstream grade**
   requires drawing from upstream to downstream with a non-increasing endpoint
   profile. Grading modifies the DTM. A raster corridor is not a zero-width TIN
   constraint or a complete drainage model.
3. Choose **Lock waterbody**, enter its known level and draw the outside boundary.
   If there is an island, click **Save this ring & draw an island**, then draw
   its exclusion boundary. Repeat for additional islands before applying.
   NoData remains missing unless filling is enabled. The level is locked through
   filtering and contour geometry is clipped from the water interior. Islands
   and boundary-coincident contours remain; known water heights are never shifted
   merely to avoid a contour interval.
4. Protected features require **average factor 1** and box/regional means off.
   The UI selects compatible settings when features are added/restored. Further
   edits that would change locked cells are rejected atomically. Remove the
   feature in its tool panel to release it; removal retains edited heights.
   Add/remove operations support Undo/Redo and automatic contour refresh.
5. Save the DTM and export **GeoPackage**. It includes `terrain_breaklines` and
   `terrain_waterbodies` vector layers, island geometry, height attributes and the
   processing/QA report in addition to contours. Features remain session state;
   reopening a TIFF/GPKG does not restore app constraints.

The change cap limits vertical generalization from the **edited** DTM when
averaging is 1. It does not establish survey accuracy, bound horizontal contour
movement or preserve every small feature automatically. The height-contrast
parameter is an adjacent-cell height difference, not a physical slope or map-scale
tolerance. A warning identifies adjacent terrain below a locked water level;
clipping alone cannot fix inconsistent shorelines/banks. The legacy
**Hydroflatten** tool remains available for a simple, replaceable raster edit.

Up to 128 protected features and one million active protected cell references are
supported, with 250,000 cells per edit, 256 vertices per line/ring and up to 16
island rings per waterbody. The 16-million-cell input limit remains. State polling
returns feature summaries; full vector geometry is fetched only when features
change. Incremental contour updates include the edge filter's full support.

## GeoPackage, live contours and constraints

**Save contours (.gpkg)** exports the current complete contour revision as an
OGC GeoPackage 1.4 file, directly from the Go application. It contains:

- `contours`: source-CRS LineStrings with elevation, index status, contour
  interval, declared elevation units, revision, area where known, and a NULL
  depression field until depression classification is implemented.
- A spatial R-tree index on the contour layer with standard maintenance triggers
  for subsequent GIS edits. CRS definitions are included from the bundled PROJ/EPSG catalog.
- `terrain_report`: parameters, surface-change QA, refresh timings and contour
  elevation constraints. Unknown CRS remains explicitly undefined; projected
  coordinates are never labelled as EPSG:4326 just because the base datum is WGS84.

CSV/WKT and contour-surface GeoTIFF export remain available. Export is disabled
while the current contour revision is unavailable or being refreshed. No Python,
GDAL installation, separate SQLite executable or network access is needed to
export a GeoPackage from the application.

With **Automatically refresh after DTM changes** enabled (the default), edits,
Undo and Redo regenerate the contours in the background. Further DTM editing is
allowed during refresh; newer edits cancel obsolete work. Old outlines may remain
faded temporarily, but stale geometry cannot be exported as the current revision.
A short debounce batches rapid edits. Settings changes trigger a full rebuild;
local DTM changes reuse unaffected contour tiles, with complete smoothing halos.

Choose **Move/pin contour**, set a constraint radius, then drag a contour vertex
and release. The DTM is adjusted locally to the contour's elevation at the new
location, and the derived contour regenerates. This changes the terrain; it is
not an independent vector drawing disconnected from the DTM. The surrounding
change feathers out within the radius. Protected samples retain the pin through
rounding and smoothing. A conflicting DTM edit is rejected atomically; remove its
pin in the tool panel before intentionally changing that terrain. A move, pin
removal, Undo or Redo remains one edit-history action. Escape cancels a drag.

Pins require **block average factor 1**, valid surrounding elevations and a
nonzero local slope. The radius is 2–50 source pixels; up to 256 pins can be
active in a session. Known water levels and existing pins cannot be overwritten
by another pin. Constraints are session state: save the edited DTM and the
GeoPackage/report to preserve terrain and an audit record before closing. Opening
an exported TIFF does not reload pins; a reloadable constraint project is not
implemented. Contour CSV/GPKG exports do not automatically reload as editable
application sessions either.

### Responsiveness and scope

The engine caches 128×128 contour-cell tiles, retraces affected tiles, and rejoins
geometry across tile edges. Up to four workers are used for tracing. A focal mean
uses running sums rather than a nested neighbourhood loop. The viewer caches
canvas paths and reuses the unchanged source preview; state polling does not
rescan the immutable source elevation range on every request.

The viewer combines redraw requests into one animation frame and retains its
canvas allocation between paints. Contour display preparation yields during
large geometry loads and skips distant vertex groups when selecting a handle.
Unchanged history is retained between polls. **Cancel refresh** stops contour
work; changing the DTM or settings can start a new refresh afterwards.

On the onboarding Linux machine (Xeon Platinum 8573C), three-iteration backend
benchmarks averaged approximately **288 ms for a full 4.2-million-cell run**,
**112 ms for a local update on that grid**, and **503 ms for a local update on
16 million cells**. These use a simple sloping surface, 1-unit contours and a
3×3 mean. They exclude edit snapshot copying, previews, network transfer,
GeoPackage export and browser rendering. Dense/noisy contours and other machines
can be slower; these are measured examples, not latency guarantees.

Input remains limited to **16 million cells / 512 MiB / classic TIFF**. Contour
output is limited to 2 million traced segments and 4096 elevation levels. This is
an in-memory engine, not an out-of-core mosaic processor. Local updates still copy
a surface snapshot, compute whole-grid QA and rejoin the current segment graph;
very large or dense datasets need cropping, suitable averaging or a larger
interval. Larger-than-limit processing and viewport-only streaming remain work
for a future implementation.

### Report compliance

[The complete recommendation audit](docs/report-audit.md) records implemented,
partial and missing stages, with report page references. **The complete USGS
hydrographic/cartographic workflow is not implemented.** In particular, automatic
water/river enforcement, PAEK/vector smoothing, depression classification,
buffered AOI processing and hydrographic proximity QA remain missing. A smooth
appearance or the availability of GeoPackage export does not establish those
capabilities or surveyed accuracy.

## GIS workspace and manual editing


### USGS-inspired contour surface treatment

The **USGS-inspired · round then filter** preset implements an initial part of
[USGS SIR 2012–5167](https://pubs.usgs.gov/sir/2012/5167/): round elevation values
before a 3×3 focal mean to reduce tiny oscillating contours on nearly flat terrain
(pp. 18–21). It runs on a separate contour surface, leaving the input and edited
DTM unchanged. It retains the input resolution and disables size-based contour
removal so real small landforms are not hidden automatically.

1. Open or prepare a DTM. Set the required contour interval and base.
2. In **Contours**, declare the actual **source elevation units**. This records
   units; it does not convert the raster. Horizontal CRS units do not establish
   vertical units.
3. Select **USGS-inspired · round then filter**. The rounding step becomes
   0.03048 for metre elevations, 0.1 for international feet, or the equivalent in
   US survey feet. If height units are unspecified, rounding stays disabled;
   enter a suitable step explicitly or declare units. While the preset is active,
   changing that declaration updates its rounding step automatically.
4. Compare contours with the normal/full-detail surface. Inspect banks, ridges,
   embankments and known depressions. A finer grid or smoother line does not prove
   better ground accuracy. Excessive rounding can remove real relief.
5. Save the contour surface, CSV/WKT lines and **contour quality report**.

The report records RMS, signed mean and maximum absolute surface change from the
edited DTM, comparison/masked/cropped cell counts, protected water blocks, removed
loop/line counts, parameters and limitations. For an averaged grid, each covered
source cell is compared with its corresponding contour block value. These are
measures of generalization, **not survey accuracy or vertical RMSE against control**.
Contour QA is also included in the existing edit/run report. It is invalidated by
DTM changes and regenerated with the contour result.

**Preserve manually hydroflattened levels** is enabled by default. Cells marked
by the Hydroflatten tool retain their exact known elevation through rounding,
focal and regional filtering. When averaging, a block touching water takes that
water elevation only if the whole block is valid; a block containing conflicting
water levels is rejected with instructions to reduce averaging. NoData stays
NoData. A later height-changing non-water edit clears the affected constraint;
Undo/Redo restores it. Already-level cells selected with Hydroflatten acquire a
constraint too. Protection belongs to the current editing session; exporting and
reloading a DTM does not retain its edit flags.

This is an initial report-inspired surface treatment, **not the complete USGS
workflow**. Automatic hydrography import, stream-direction checks, river grading,
shoreline routing/buffering, depression classification, tile edge matching,
TopoToRaster, PAEK line smoothing and vector simplification are not implemented.
Water levels are not shifted automatically to avoid contour elevations; a quality
note flags a protected level coincident with a contour. Manual hydroflattening
alone does not establish conformity to lake polygon boundaries or river channels.

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
at raster centre boundaries and NoData gaps. Elevation rounding, when enabled, precedes the focal mean; protected manually
hydroflattened cells are restored after each filtering stage. No vector-line
smoothing is applied.

Limits: at most 4096 elevation levels and 2 million traced segments per generation.
Increase interval/averaging/smoothing if limits are reached. Settings use raster
height units, so a 1 m contour interval requires metre elevation values (use an
appropriate converted interval for feet). Averaging factors are integer 1–100;
the smoothing radius is 0–5 (none to 11×11). The app does not invent finer terrain
resolution by upsampling coarse input.

Save the contour surface as a georeferenced float32 TIFF, the lines as GeoPackage, or as CSV
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

* **Repair spikes:** click for a circular brush, or hold the left mouse button
  and drag to paint a larger area (radius 1–50 source pixels). The translucent
  overlay shows the selected footprint; release to apply. Every valid covered
  cell is compared with its pre-stroke 5×5 neighbourhood median (excluding itself)
  and replaced only if its absolute deviation exceeds the threshold. Both high
  and low spikes are repaired; 0 repairs all differing covered cells. The brush
  includes the full path between mouse events, overlapping passes visit cells
  once, and cells outside its footprint remain unchanged. NoData is skipped.
  One completed stroke is one Undo/Redo action. Escape, pointer cancellation or
  losing window focus cancels the uncommitted stroke. Middle-drag still pans;
  zooming is paused during painting to keep the selected footprint stable.
  No-op strokes report that no spikes exceeded the threshold without creating
  an undo entry. Stroke centres are clipped to raster bounds. Strokes are limited
  to 256 path points and 250,000 selected cells;
  excessively large strokes are rejected without partial changes. This is local
  spike repair, not removal of broad raised objects or recovery of unknown ground.
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
source cells per action. Polygon selections also have a 2-million-cell bounding-box limit; self-crossing
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
32 interpolated cell retained in exports, 64 manually edited, 128 hydroflattened, 256 pinned contour elevation, 512 protected breakline,
1024 locked waterbody.
Filled cells combine bits 16 and 32. Other flags may also combine.
These are raster cell flags, not LAS point classifications.

## Input/output limits

Classic TIFF, single band, integer 8/16/32/64 or float32/64, little/big endian,
strips or tiles; uncompressed, Deflate or TIFF LZW; predictors 1/2/3.
RGB and multiband images are rejected. Supports a maximum of 16 million cells
and 512 MiB input files. Processing is in memory; extraction is single-job, while background contours allow further DTM edits; large
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

Requires Go 1.26 or newer, Wails v2 and Windows WebView2 for the desktop build.

```powershell
go test ./...
.\Build-Terrain-Studio.ps1
& ".\dist\Terrain-Studio.exe"
go run . -input "C:\data\elevation_dsm.tif" -output "C:\data\terrain-results" -config terrain-settings.json
```

CLI output is a directory. Existing raster output names are refused to avoid
overwriting them; use a new directory for each run. `-config` is optional.
Optional browser mode: `go run . -browser -port 8788`; `-open=false` suppresses
automatic browser launch. Sources: [CNES Bulldozer](https://github.com/CNES/bulldozer),
[TIFF 6.0 specification](https://download.osgeo.org/geotiff/spec/tiff6.pdf).


On Linux (browser interface and CLI; no desktop window):

```sh
go test ./...
go build -o dist/terrain-studio .
./dist/terrain-studio -browser -open=false
```

The Windows build can also be cross-compiled from Linux without executing it:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -tags production -ldflags '-s -w -H windowsgui' -o dist/Terrain-Studio.exe .
```

A successful cross-build is not validation of Windows dialogs or WebView2 startup.
The browser interface exercises the shared processing code and UI; Windows desktop
launch still needs checking on Windows. Building from source needs Go/module
network access, but end-user raster processing requires neither Python nor npm.
