# Validation of the first cartographic contour changes

Validated on Linux using Go 1.26.10. Source baseline: the uploaded `terrain-go`
archive; its 20 existing Go tests passed before changes.

## Completed checks

- **31 Go tests passed**, including the seven cartographic regressions for round-before-filter
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
Playwright/Chromium and Python NumPy/Rasterio respectively:

```sh
node cartographic_check.mjs
node brush_check.mjs
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
routing, PAEK/vector smoothing, depression classification and tile seam matching
remain further work. This change implements an initial report-inspired surface
treatment, rather than the full USGS production workflow.
