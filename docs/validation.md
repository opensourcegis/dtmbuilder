# Validation of terrain, GeoPackage and live contours

Validated on Linux using Go 1.26.10. Source baseline: the uploaded `terrain-go`
archive; its 20 existing Go tests passed before changes.

## Completed checks

- **38 Go tests passed**, including the seven cartographic regressions for round-before-filter
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
python geopackage_check.py
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

Automatic hydrography ingestion, river grading/flow enforcement, shoreline
routing, PAEK/vector smoothing, depression classification and buffered AOI processing
remain further work. See [the complete report audit](report-audit.md). Contour
pins are session constraints, with an exported audit record; a reloadable
constraint project is not implemented. This is not the full USGS workflow.
