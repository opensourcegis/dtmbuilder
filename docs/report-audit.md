# Audit against USGS Scientific Investigations Report 2012–5167

Reference: Tyler and Greenlee (2012), *Creation of Digital Contours That Approach
the Characteristics of Cartographic Contours*, 31 pages with appendixes.
The supplied PDF is reference material; it is not a set of workspace instructions.

**Conclusion: this application does not implement the entire report.** The
round-then-filter treatment is implemented, with explicit source height units.
Manual water protection and terrain-backed contour pins are useful extensions,
but they do not replace the report's automated hydrographic enforcement. The
following matrix distinguishes implemented functions from missing requirements
and US-specific choices that must not be silently applied to other datasets.

| Recommendation or workflow step | Pages | Actual status and evidence |
| --- | --- | --- |
| Automatic, repeatable processing; GIS-ready output | 1, 23 | **Partial.** Deterministic marching squares and recorded parameters are tested; CSV/WKT and GeoPackage exports carry elevation, interval, index and revision. Manual edits are audited. The edit report is not a reloadable project. |
| Match 1:24,000 cartographic detail while retaining small peaks | 1–4, 12–17 | **Not established.** Resolution and filters are user choices. No validation against representative surveyed terrain or a target map was supplied. Averaging can remove real small features. |
| Contour interval supported statistically by elevation accuracy | 4, 7 | **Not automated.** The user sets the interval. Grid spacing or smooth appearance does not establish vertical accuracy. Independent control is still needed. |
| Cross double-line streams approximately at right angles | 1, 3, 21 | **Missing.** There is no river-area/centre-line enforcement or in-river straightening. |
| Avoid crossing waterbody polygons | 4, 9, 18 | **Partial.** A known manually flattened water interior is protected through filtering. No automatic lake polygon import, shoreline routing, buffer or whole-polygon crossing check exists. A water level equal to a contour is flagged, not silently shifted. |
| Crenulations follow reentrants/stream channels | 4–5, 12 | **Missing.** No directed stream constraints or drainage-aware interpolator. |
| Depression contours do not cross single-line streams more than once | 4, 21 | **Missing.** No depression/stream intersection classification. |
| Handle swamps, lagoons, canals and problematic water classifications separately | 4, 8, 12 | **Missing.** Hydrographic feature types are not ingested. Do not treat the manual Hydroflatten tool as an automatic water classifier. |
| Feet, NAD83 and NAVD88 | 4–6 | **US-specific choice, not a universal default.** The app retains the source CRS and requires declared elevation units for a physical rounding preset. It does not infer or transform vertical datums. GeoPackage uses an offline EPSG WKT catalog; unknown CRS remains explicitly undefined. |
| NED points plus NHD flowlines, waterbody/area polygons and disconnected streams | 8 | **Missing.** Input is a single-band elevation GeoTIFF. There is no NHD data model/import pipeline. |
| Precompute lake minimum/maximum/mean/standard deviation | 12 | **Missing.** Manual known water elevations are used instead. |
| Detect reversed flow direction using endpoints; exclude flowlines crossing hills | 12–15 | **Missing.** No stream direction or longitudinal-profile checks. |
| Process a quadrangle with 10% overlap, then clip to its boundary | 12, 21, 24 | **Missing.** Processing covers the loaded raster. The incremental engine has exact filter-support halos, but those are not a substitute for buffered geographic input, quad clipping or cross-dataset seam matching. |
| TopoToRaster at 1/27 arc-second from 1/3 arc-second points and flowlines; five iterations | 12, 16–18 | **Missing.** Morphological DSM-to-DTM extraction, block averaging and box means are different algorithms. No TopoToRaster/ANUDEM equivalence or accuracy improvement is claimed. The app does not upsample to invent measured detail. |
| Subdivide interpolation and mosaic outputs | 18 | **Different purpose.** The app caches marching-squares tiles and reuses unaffected work after edits. It does not implement the report's 36 TopoToRaster runs or interpolation mosaic. |
| Adjust represented/unrepresented lakes using statistics and temporary contour intersections | 18 | **Missing.** Known water levels are not adjusted automatically. |
| Buffer lakes, with smaller buffers near adjacent waterbodies | 18 | **Missing.** No hydrographic polygon buffering or proximity logic. |
| Densify river centre lines, step elevations down monotonically, IDW power 0.5 / 12 points, mask river areas | 18–21 | **Missing.** Polygon patch interpolation uses a different method and is not river grading. |
| Mosaic terrain, river IDW surface, then waterbody surface | 21 | **Missing.** There are no separate hydrographic mosaics. |
| Multiply, round, divide, then filter to suppress “wormwood” | 21 | **Implemented as an option.** Rounding precedes the focal mean. The preset uses 0.1 international foot = 0.03048 metre, with explicit elevation units. Regression tests distinguish this from filter-then-round and confirm NoData preservation and plateau-noise reduction. Known water and contour-pin samples are deliberately preserved as exact constraints. |
| Generate contours on the prepared elevation surface | 21 | **Implemented.** Marching squares resolves saddles, joins across computation tiles, and stops at NoData. Full/incremental results are compared in regression tests. This does not imply equivalence to ArcGIS Contour for every degenerate surface. |
| Point-remove simplify → PAEK smooth → simplify | 21–22 | **Missing.** There is no vector simplification or PAEK smoothing. Cached canvas paths accelerate display; they do not alter exported geometry or substitute for those algorithms. |
| Clip in-river contour pieces, collapse them to straight crossings, append back | 21 | **Missing.** Requires river-area polygons. |
| Polygonize contours, transfer left/right attributes, classify depressions | 21, 24 | **Missing.** GeoPackage depression is NULL (unknown), not falsely labelled as a hill or depression. |
| Remove very small polygonal contours | 21 | **Implemented generically, not calibrated to this report.** Minimum enclosed area is in m² and requires recognized projected units. The legacy normal preset's 50 m² is a project choice, not a threshold specified in the report. The report-inspired preset disables size filtering for comparison. |
| Remove short depression contours crossing streams while preserving disconnected drainage depressions | 21 | **Missing.** General minimum-length filtering is not an equivalent operation. |
| Clip back to AOI; retain contour interval, units and attributes | 21, 30 | **Partial.** GeoPackage contains source-CRS geometry, elevation, interval, units, index, area where known and revision. AOI polygon clipping, depression classification and the complete USGS data dictionary are not implemented. |
| QA near areas/lakes using raw/final counts and two-interval discrepancies | 21, 23 | **Missing.** Global surface-change statistics do not replace these hydrographic proximity checks. |
| Reconstruct a surface from contours and flag flowlines near discrepancies over three intervals | 23 | **Missing.** No contour-to-surface or flowline QA stage. |
| Visually inspect flagged areas, edit input hydrography, regenerate | 23 | **Partial.** Manual DTM/contour-pin editing and automatic regeneration exist. Flag generation and hydrography editing do not. Moving a contour modifies and constrains the DTM; there is no independent vector-only edit that silently disagrees with the terrain. |
| Combine states/quads, remove coastal zero contours, propagate depression status | 23–24 | **Missing.** No multi-dataset coastal or depression post-processing. Zero contours must not be deleted automatically for inland/negative terrain. |
| Explicitly join features across quadrangle boundaries | 4 | **Not part of the reported implementation.** The report explicitly says edge-matching and joining across quad boundaries were not attempted. Its 10% processing overlap is a separate, implemented precaution. Cross-dataset feature joining would be an additional application capability. |

## Recheck of suitability for topographic contours

The current application **partly follows the report; it is not an implementation
of its complete cartographic workflow**. The code and targeted regression tests
were checked again against the supplied report. The following distinctions matter
when assessing an actual contour output:

- **The treatment is optional.** The initial "Normal DTM contours" preset has
  rounding and focal filtering disabled and a 50 m² minimum loop area when the
  raster has recognized projected coordinates (otherwise the preset uses 0). To use the
  implemented report-inspired treatment, first declare source elevation units,
  then select "USGS-inspired · round then filter". This selects 0.03048 m or
  0.1 international foot rounding, a 3×3 focal mean and no size filtering. Changing
  the unit declaration does not convert elevations. While this preset is active,
  changing the declaration automatically updates the rounding step.
- **Only the operation order and rounding increment are reproduced.** Page 21
  specifies round-to-tenth-of-a-foot followed by filtering. It does not specify
  the filter kernel/window there. The application's 3×3 arithmetic mean is an
  implementation choice, not a verified reproduction of the report's filter.
  Exact protected water/pin samples are also deliberate departures from uniform
  rounding/filtering.
- **Raster smoothing does not provide the reported vector generalization.**
  The report explicitly simplifies, applies PAEK and simplifies again (pp. 21–22).
  All three vector stages are absent. Thus contours may retain angular raster
  detail despite a smoother elevation surface.
- **Hydrographic correctness is not established by manual flattening.** The app
  cannot yet guarantee no lake crossings, drainage-aligned reentrants, downhill
  river surfaces or approximately perpendicular double-line river crossings.
  Those need the missing hydrographic inputs, conditioning and checks.
- **Direct contour editing is an application extension.** The report's automatic
  process edits NHD inputs and regenerates contours (pp. 1, 23). Here, a drag
  changes the DTM and locks four samples locally. That maintains the local
  terrain/contour relationship, but does not establish drainage consistency,
  survey accuracy or the report's full cartographic rules.
- **Depression and accuracy QA remain incomplete.** Closed loops are not
  classified as hills or depressions. The QA RMS is a change from the edited DTM,
  not error against control. No automatic test establishes that the chosen
  interval is statistically supported, or reproduces the report's two-/three-
  interval hydrographic discrepancy checks (pp. 4, 21, 23).
- **Use the source project's CRS and vertical datum.** NAD83/NAVD88, geographic
  tolerances and US quadrangle dimensions in this report are dataset-specific;
  they should not be copied to an Indian project. GeoPackage preserves source
  horizontal coordinates; it does not perform vertical datum conversion.

The next substantive work for closer agreement is hydrographic input and terrain
conditioning, vector generalization with displacement/topology checks, depression
classification, buffered AOI processing and hydrographic QA. Independently assess
representative contours against the DTM, drainage, spot heights and control at the
intended map scale before treating them as a finished topographic product.

## Improvements delivered with this audit

- Native GeoPackage 1.4 export with CRS definitions, attributes, a spatial index,
  maintenance triggers and a snapshot of processing/quality/constraint metadata.
- Background contour jobs permit further DTM edits. A newer edit cancels obsolete
  work; outdated results cannot replace the current revision or be exported.
- Affected surface patches are recomputed with complete smoothing halos; affected
  contour tiles are retraced and rejoined globally. Unchanged tiles are reused.
- Direct contour-handle moves adjust the DTM and protect the local elevation
  constraint through later filtering. Conflicting terrain edits are rejected;
  removing pins and Undo/Redo are explicit operations.
- Corrected PixelIsPoint cell-centre coordinates and averaged-grid referencing.

These changes do **not** close the missing hydrographic/cartographic stages above.
To validate those stages in a future implementation, representative elevation,
lake, river-area, directed flowline and disconnected-stream data are needed, along
with target scale, contour interval, horizontal/vertical CRS and independent
accuracy criteria. Absence of those data does not prevent using the features
implemented here, but it prevents a claim of full report compliance or accuracy.
