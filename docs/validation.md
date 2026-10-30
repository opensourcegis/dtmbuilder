# Validation of terrain, GeoPackage and live contours

Validated on Linux using Go 1.26.10. Source baseline: the uploaded `terrain-go`
archive; its 20 existing Go tests passed before changes.

## Completed checks

- Bug-review regressions reject backtracking breaklines atomically, block edits during raster replacement, and clear the previous contour overlay when a new raster loads.

- **50 Go tests passed**, including the seven cartographic regressions for round-before-filter
  order, plateau-noise contours versus meaningful relief, water constraints
  through rounding/focal/regional filters and averaging, constraint history,
  source coverage/change measurements, invalid input/cancellation/empty surfaces,
  report export and invalidation after terrain changes.
- Four brush regressions check continuous/sparse stroke coverage and circular
  ends, overlapping/retraced strokes, high/low repairs with outside/NoData
  protection, atomic Undo/Redo, thresholds/no-op feedback and work limits.
- A browser brush regression uses an independent TIFF fixture to check the full
  swept area, sparse mouse events, high/low spikes away from the stroke centre,
  untouched outside/NoData cells, one Undo/Redo, Escape/pointer cancellation,
  single-click footprint, stable view while painting and middle-button panning.
- `go test -race ./...`, `go vet ./...`, and `go mod verify` passed.
- Headless Chromium/Playwright exercised the real browser application: metre,
  international-foot, US-survey-foot and unspecified-unit preset values; contour
  generation without changing the DTM; water polygon editing and preservation;
  report download; removal of stale QA; automatic regeneration; contour-surface
  TIFF export; and surface hillshade. No browser JavaScript errors were recorded.
- An independent Rasterio read of the browser-exported TIFF confirmed retained
  NoData and exact float32 known-water elevations in the selected rectangle.
- The existing Rasterio interoperability script passed five input combinations:
  uncompressed little-endian, tiled Deflate predictor 3, tiled LZW predictor 3,
  big-endian LZW predictor 1, and Deflate predictor 2. It checked georeferencing,
  NoData, finite terrain heights and synthetic building removal.
- Linux browser/CLI binary built and started successfully, and its functional
  requests completed. Windows x86-64 GUI binary cross-compiled successfully with
  production assets, trimmed source paths and stripped debug information.
- JavaScript syntax checks and `git diff --check` passed.

The plateau regression removes tiny contours through surface treatment with both
size filters disabled; it does not hide them by raising the minimum loop area.
The water regression checks that no contour enters the interior of the protected
flat-water fixture. Neither fixture establishes conformity to arbitrary lake
boundaries, river channels, or surveyed ground.

## GeoPackage, live editing and incremental refresh

- GeoPackage header/version, required tables, geometry envelopes/WKB, source CRS,
  attributes, nullable depression classification, SQLite integrity, spatial-index
  population and seven standard index-maintenance triggers passed Go tests.
- Offline EPSG definitions, undefined source coordinates, custom projected CRS
  handling and PixelIsPoint referencing (including averaging) passed tests.
- Tiled tracing matches the original full-grid marching-squares reference.
  Incremental updates match full regeneration for averaging factors 1/2, focal
  radii 0/1/5, regional radii 0/2, NoData, tile boundaries and raster edges.
- Background jobs allow DTM edits, reuse unaffected tiles and refuse to publish
  obsolete revisions. Incremental quality counters match a full recount.
- Contour dragging constrains the DTM at the contour height, survives rounding
  and smoothing, rejects conflicting edits atomically and supports pin removal
  and Undo/Redo.
- A live browser regression exercised direct handle dragging, pinned DTM heights,
  automatic incremental refresh after polygon editing, conflict rejection,
  unpin/Undo and GeoPackage download. The regenerated contour passed within
  0.01 source pixels of the requested pinned location in the fixture.
- Independent GDAL/Fiona opened the downloaded EPSG:32643 GeoPackage, checked
  feature geometry/attributes/revision and constraint metadata, then appended a
  feature to a copy. The spatial index updated correctly through its triggers.

Three-iteration backend benchmarks on this Linux Xeon Platinum 8573C machine:

| Grid/workload | Mean time | Allocated per operation |
| --- | ---: | ---: |
| Full contours, 2048×2048 cells | 288 ms | 103 MB |
| Local contour update, 2048×2048 cells | 112 ms | 37 MB |
| Local contour update, 4000×4000 cells | 503 ms | 144 MB |

These use a simple slope, 1-unit interval and 3×3 focal mean. Timings exclude
DTM edit snapshot copying, preview generation, transport, GeoPackage export and
browser rendering. They are not Windows latency guarantees. Dense/noisy terrain
can be slower. The 16-million-cell input limit remains; this is an in-memory
engine and still performs whole-grid quality comparisons and global line joining.

## UI and responsiveness review

The following fixes passed the additional `ui_performance_check.mjs` browser check:

- Opening the unreferenced demonstration with default settings now generates
  contours; the Normal preset uses zero minimum area when physical area cannot
  be determined. Other presets do not inherit a previous small-loop threshold.
- The active report-inspired preset updates its rounding increment when the
  declared height unit changes. This does not convert the elevation raster.
- Identify responses arriving out of order cannot replace the latest cell or
  reuse heights from an older terrain revision. Undo/Redo invalidate the inspector.
- Cancelled file selection resets the DSM/DTM mode; the same file can be selected
  again. Drag-and-drop respects unsaved-edit confirmation, and replacement
  controls are blocked during an active edit. Uploaded Unicode/spaced filenames
  are decoded for display.
- Both the contour panel and processing panel can cancel a contour job. The
  cancellation endpoint invalidates the job and clears its progress/status;
  a new Go regression covers that transition.
- A burst of 120 redraw requests produced one paint and no canvas size writes
  at the existing viewport size. The viewer retains its backing store until
  dimensions change, clears it before painting and shares an animation frame.
- Preparing a synthetic 400,000-vertex display path yielded to UI callbacks and
  retained its complete coordinate extent. Geometry download/JSON parsing and
  actual canvas strokes are outside that preparation check. Hit testing uses
  small vertex-group bounds to skip distant points; live dragging still passed.
- Unchanged history nodes survive repeated polling. Obsolete contour downloads
  are aborted, and path preparation checks that its generation is still current
  before publishing.
- At 900×700, the page has no horizontal overflow, the viewer fits above the
  footer and the toolbar scrolls horizontally. The flag legend includes contour
  pins, hydroflattening and manual edits with the correct colour priority.

Existing live-contour, GeoPackage/GDAL, cartographic-water and brush browser
regressions also passed after the review. These checks use headless Chromium on
Linux; they do not establish native Windows behaviour or a universal frame rate.

## Research-based drawn terrain constraints

- The bounded edge filter reduced synthetic plateau-noise contours with both
  size filters disabled, retained a five-unit bank, preserved NoData and kept
  changes within the specified source-height cap. This is not survey validation.
- Graded/preserve breaklines retained their protected samples through rounding
  and filtering. A continuous fixed corridor blocked diffusion from one side to
  the other. Self-crossing lines and uphill downstream endpoints were rejected.
- Scanline water selection matched independent point-in-polygon membership for
  concave, slanted and half-cell boundary fixtures. A full API request with 16
  islands of 256 vertices each passed; its payload exceeds the former 128 KiB
  request limit and is accepted within the new 512 KiB limit.
- Waterbody tests covered island exclusions, NoData, conflicting known levels,
  self-crossing/overlapping invalid rings, filter/averaging restrictions and
  protected samples in cropped edge margins.
- Vector clipping retained outside pieces, island contours and shoreline-
  coincident segments, and respected cancellation. A low-bank diagnostic flagged
  terrain inconsistent with a supplied water level. Its threshold is an app
  diagnostic, not the report's hydrographic discrepancy test.
- Feature addition/removal and Undo/Redo retained constraints and terrain
  consistently. Audit entries do not retain hidden history snapshots; branching
  history releases discarded Redo accounting.
- A browser workflow drew preserve and downstream-grade breaklines, then a level
  waterbody with an island. It verified endpoint heights, zero protected-sample
  mismatches, 3 retained island contours, zero water-interior segments, atomic
  rejection of a conflicting edit, removal/Undo and DTM/GPKG exports.
- Independent GDAL/Fiona/Shapely/Rasterio checks opened the contour, breakline and
  waterbody layers, checked source CRS and height attributes, retained the island
  polygon, found zero contour length inside open water, and confirmed the saved
  DTM's known water and endpoint heights.
- Full and incremental edge-filter surfaces and line geometry matched at tile
  boundaries and raster edges. State polling returns summaries, with vector
  feature geometry fetched only on feature-generation changes.

Three-iteration constrained-contour backend benchmarks on the same Xeon machine:

| Grid/workload | Mean time | Allocated per operation |
| --- | ---: | ---: |
| Full constrained contours, 2048×2048 | 776 ms | 104 MB |
| Local constrained update, 2048×2048 | 145 ms | 42 MB |
| Local constrained update, 4000×4000 | 459 ms | 152 MB |

These include four edge passes with a 0.5-unit contrast, a 0.2-unit change cap,
a protected breakline, a 300×300-cell waterbody, water clipping and bank QA on a
simple slope. They exclude edit snapshot copying, previews, transfer, rendering
and export; dense geometry and other machines can be slower. The initial full
edge-filter run costs more than the box-mean baseline. These measurements support
incremental reuse, not an out-of-core or universal responsiveness claim.

See [the research review](contour-research.md) for the papers, access limitations,
algorithm details and the distinction between an independent adaptation and a
reproduction of published normal-vector smoothing.

## Reproduction

```sh
go test -v ./...
go test -race ./...
go vet ./...
go mod verify
go build -o dist/terrain-studio .
./dist/terrain-studio -browser -open=false -port 8789
```

Run optional integration checks in another terminal. They need development-only
Playwright/Chromium and Python NumPy/Rasterio/Fiona respectively:

```sh
node cartographic_check.mjs
node brush_check.mjs
node live_contours_check.mjs
node ui_performance_check.mjs
node bug_review_check.mjs
node constrained_terrain_check.mjs
python geopackage_check.py
python constrained_terrain_gis_check.py
python fixture_check.py
```

`STUDIO_URL` changes the browser test destination. `PLAYWRIGHT_MODULE` can point
to an external Playwright `index.mjs`, and `CHROMIUM_PATH` can select a system
Chromium binary. `CHROMIUM_NO_SANDBOX=1` is an opt-in for isolated test containers
that cannot run Chromium's sandbox. `TERRAIN_EXECUTABLE` overrides the fixture
script's default platform-specific binary in `dist/`.

On Windows, `Build-Terrain-Studio.ps1` runs Go tests and packages the desktop
binary with its launcher and SHA-256 checksum. The checked-in GitHub Actions
workflow defines native Linux/Windows test and build jobs. Adding a workflow is
not evidence that a hosted run has completed.

## Not validated or not implemented

Windows desktop launch, native file dialogs and WebView2 behaviour have **not**
been exercised in this Linux environment. No user GeoTIFF crop or surveyed control
points were supplied; no project-specific accuracy improvement is claimed.

Automatic hydrography ingestion, full river-area grading/network flow enforcement, shoreline
routing, PAEK/vector smoothing, depression classification and buffered AOI processing
remain further work. See [the complete report audit](report-audit.md). Contour
pins and drawn terrain features are session constraints, with exported geometry/audit records; a reloadable
constraint project is not implemented. This is not the full USGS workflow.
