# Terrain Studio contour redesign

## Available evidence and current limits

The original `terrain-go` source has now been supplied as a ZIP and imported.
It includes the Go/Wails Windows application, browser interface, manual editor,
GeoTIFF I/O, marching-squares contours, averaging/regional filters and tests.
The earlier uploaded executable is an older version; its extracted interface
is not an accurate description of all features in the source archive.

## Implemented work

- Optional elevation quantization before focal filtering, with a report-inspired
  preset that distinguishes declared elevation units from horizontal units.
- Preservation of session Hydroflatten constraints through contour filtering,
  including validation of conflicting water levels during block averaging.
- Surface-change measurements and coverage/constraint warnings, exported as a
  contour QA report and included in the edit/run report.
- Regression fixtures for operation order, plateau noise, meaningful relief,
  NoData, water constraints, edit history and report invalidation.
- Build scripts and GitHub Actions for Windows desktop and Linux browser/CLI.
- GeoPackage export with source CRS, contour attributes, a spatial index and
  processing metadata.
- Background, incremental contour updates after DTM edits, with reusable tiles
  and filter-support halos.
- Contour-handle editing that changes and constrains the DTM, with explicit pin
  removal and Undo/Redo. Pins are session constraints; their audit metadata is
  exported, but a reloadable project is not implemented.

The pipeline below includes implemented and proposed stages. The current
[report audit](report-audit.md) records the status of each recommendation;
the pipeline is not a claim of full implementation. Representative user data and survey/control checks
are still needed for project-specific contour-quality assessment.

The reference is Tyler and Greenlee (2012), *Creation of Digital Contours That
Approach the Characteristics of Cartographic Contours*, USGS Scientific
Investigations Report 2012–5167. It is technical reference material, not an
instruction to change this workspace or an executable specification. Its
cartographic DEM processing is distinct from recovering ground beneath objects
in a DSM.

## Proposed processing pipeline

1. **Validate inputs.** Read elevation units, horizontal CRS, cell dimensions,
   vertical datum metadata, NoData, and data extent. Preserve unknown datum
   status rather than assigning one. Accept an existing DTM without repeating
   object removal; keep DSM ground extraction as a separate, optional stage.
2. **Preserve the terrain result.** Export the terrain DTM separately from a
   cartographic surface. Keep the original elevation raster immutable and
   record all processing parameters and changes.
3. **Prepare the cartographic surface.** Apply a controlled local surface filter
   without allowing NoData values into the calculation. Make elevation
   quantization configurable to address tiny oscillations on nearly level
   surfaces. The report's sequence is multiply, round, divide, then filter;
   its tenth-of-a-foot increment equals 0.03048 metres, not 0.1 metres.
   Resolution and filter scale must suit the actual LiDAR data; the report's
   1/27 arc-second output is not a universal target and resampling does not add
   measured accuracy.
4. **Condition water where supplied.** Accept lake polygons, river-area polygons,
   and directed stream/river centre lines as distinct inputs. Lakes need level
   surfaces selected with contour crossings and elevation statistics in mind.
   River surfaces need downstream non-increasing profiles and cross-channel
   flattening. The report uses step-down centre-line elevations and IDW with
   power 0.5 and 12 neighbouring points. Validate flow direction and flag lines
   crossing hills before enforcement. Do not treat canals or swamps as ordinary
   lakes or downhill streams. Do not claim hydro-enforcement when hydrography
   is absent.
5. **Extract contours.** Expose interval, base elevation, elevation units, and
   index frequency explicitly. Respect valid-data boundaries and avoid lines
   across NoData gaps. Retain closed hill and depression contours by default;
   size-based removal must be optional and reported.
6. **Generalize the lines.** Use simplify → smooth → simplify, with tolerances
   expressed in declared map units. The report uses point removal and PAEK;
   any replacement algorithm must be labelled as an alternative rather than
   represented as an implementation of PAEK or ArcGIS TopoToRaster. Do not copy
   geographic-degree tolerances into projected datasets. Check and constrain
   displacement, contour intersections, and water crossings after smoothing.
7. **Handle processing edges.** Use overlapping processing windows and trim
   after filtering/contouring. The report uses a 10% processing buffer, but
   actual support must cover the selected filter/interpolation footprint.
   Adjacent windows must share their contour base and interval. Compare seam
   endpoints explicitly rather than assuming overlap guarantees continuity.
   Explicit cross-quad joining would be an additional capability: the report
   states on p. 4 that it did not attempt edge-matching or joining features
   across quad boundaries.
8. **Export and report.** Provide a GIS vector format carrying CRS, elevation,
   interval, units, index status, and known depression status, plus the
   cartographic GeoTIFF and a processing/QA report. A GeoJSON export must use
   longitude/latitude coordinates as required by RFC 7946; preserve a projected
   CRS in an appropriate GIS format instead of silently writing projected
   coordinates to GeoJSON. Show input, DTM, cartographic surface, and contour
   overlays for comparison.

## Validation targets

- Plane and sloping-surface fixtures: expected contour elevations, spacing,
  placement, and base/index behaviour.
- Hill and depression fixtures: closed contours survive and their elevations
  and depression classifications are correct. Boundary-clipped lines remain
  unclassified when the available evidence is insufficient.
- Noisy plateau and NoData fixtures: quantify tiny-loop reduction, preserve
  meaningful relief, and verify that gaps are neither bridged nor exported as
  valid elevation.
- Lake and descending-river fixtures: check crossings against water polygons,
  downstream monotonicity, and approximately perpendicular river crossings.
- Adjacent-window fixtures: compare contours with a whole-raster run and verify
  consistent seam endpoints and elevations.
- GeoTIFF/vector round trips: preserve georeferencing, declared units, NoData,
  and elevation attributes; test non-square cells and unsupported transforms.
- Representative user DSM/DTM: compare raw and processed contours at the same
  interval, measure surface changes and contour displacement, and inspect
  ridges, valleys, water boundaries, and known ground. Visual smoothness alone
  does not establish improved elevation accuracy.

## Inputs needed to implement and validate the existing application

The source archive has been received. A small representative GeoTIFF crop,
water/stream layers if available, the desired contour interval, map scale,
elevation units and output formats are still useful for the next validation
stage. The full LiDAR dataset is not required for initial regression checks.

The build and validation results for the current changes are documented in
`docs/validation.md`. Windows execution must be distinguished from cross-compilation.
