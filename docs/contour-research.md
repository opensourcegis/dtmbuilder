# Research basis for contours from high-resolution DTM

Reviewed 29 October 2026. This is a focused implementation review, not an
exhaustive literature search or a claim of surveyed accuracy.

## Papers and what they contribute

| Work | Relevance | Application here |
| --- | --- | --- |
| Tyler, D.J. & Greenlee, S.K. (2012). *Creation of Digital Contours That Approach the Characteristics of Cartographic Contours*. USGS SIR 2012–5167. [Report](https://doi.org/10.3133/sir20125167) | Cartographic contours need hydrographic consistency, controlled surface preparation and explicit QA. Rounding before filtering and vector generalization are separate stages. | The uploaded full report was read. Its existing optional round-then-filter treatment remains. Drawn water polygons now protect the known level, exclude interior contours and preserve island exclusions. These additions do not implement the full report. |
| Lindsay, J.B., Francioni, A. & Cockburn, J.M.H. (2019). *LiDAR DEM smoothing and the preservation of drainage features*. **Remote Sensing 11(16), 1926**. [DOI](https://doi.org/10.3390/rs11161926) | Directly relevant to removing short-scale LiDAR roughness while retaining drainage and breaks in slope. | Motivates feature-aware surface treatment and evaluating relief/drainage retention rather than smooth appearance alone. Its published normal-vector smoothing method is **not** implemented here. |
| Sun, X., Rosin, P.L., Martin, R.R. & Langbein, F.C. (2007). *Fast and Effective Feature-Preserving Mesh Denoising*. **IEEE TVCG 13(5), 925–938**. [DOI](https://doi.org/10.1109/TVCG.2007.1065) | A basis for feature-preserving normal-field filtering; later adapted to raster terrain. | Supports the distinction between ordinary averaging and feature-preserving denoising. This application does **not** reproduce Sun's mesh/TIN normal-field algorithm. |
| Perona, P. & Malik, J. (1990). *Scale-space and edge detection using anisotropic diffusion*. **IEEE TPAMI 12(7), 629–639**. [DOI](https://doi.org/10.1109/34.56205) | A canonical edge-aware filtering formulation. It is an image-processing paper, not a cartographic contour standard. | Implemented as a bounded, four-neighbour **elevation adaptation** with fixed terrain samples, finite support and incremental halos. It is an alternative to the normal-field method above, not an equivalent implementation. |

## Access and evidence limits

Publisher pages, DOI resolution, Crossref/OpenAlex and public search endpoints
were blocked by this environment's network proxy. The uploaded USGS PDF was
available in full. For the other works, the accessible technical descriptions and
bibliographic references used were:

- The first-author WhiteboxTools [FeaturePreservingSmoothing documentation in
  source](https://github.com/jblindsay/whitebox-tools/blob/master/whitebox-tools-app/src/tools/terrain_analysis/feature_preserving_smoothing.rs),
  which identifies the 2019 LiDAR paper and explains the distinction between
  normal-field filtering, elevation reconstruction and the original Sun method.
- GRASS [r.denoise documentation](https://github.com/OSGeo/grass-addons/blob/grass8/src/raster/r.denoise/r.denoise.md),
  which provides the Sun DOI and discusses ridge preservation and the danger of
  retaining noise when the feature threshold is too restrictive.
- OpenCV [anisotropic diffusion documentation](https://github.com/opencv/opencv_contrib/blob/4.x/modules/ximgproc/include/opencv2/ximgproc.hpp),
  which states the Perona–Malik equation and both common conductance functions.
- GRASS [v.generalize documentation](https://github.com/OSGeo/grass/blob/main/vector/v.generalize/v.generalize.md),
  which distinguishes simplification from smoothing and documents changes in
  vertex count and geometry. It is implementation documentation, not a paper.

The three non-USGS papers' full texts were **not** retrieved or read in this
session. Their performance/accuracy results are not represented as validated
for this application. These accessible descriptions are enough to justify the
explicitly labelled adaptation and its tests, not a claim of reproducing those
papers. All new numerical and geometric implementation code is independently
written; external implementation source is not copied.

## Implemented method

The new **High-resolution DTM · constrained edge filter** preset keeps source
resolution, disables box/regional means and size-based line removal, and starts
with four diffusion passes. The adjacent-cell height contrast starts at half
the selected contour interval; the maximum surface change starts at one fifth
of the interval. These are **application starting values**, not values prescribed
by the papers or evidence that the contour interval is supported by accuracy.
Review them against the actual DTM, landforms and intended map scale.

For an unfixed valid cell, each pass uses:

```
z_next = z + 0.2 * sum_over_four_neighbours[(z_neighbour - z) /
                         (1 + ((z_neighbour - z) / contrast)^2)]
```

The time step is below the four-neighbour explicit diffusion stability bound of
0.25. NoData neighbours contribute nothing; missing centres remain missing.
The contrast is in source elevation units **per adjacent grid step**, not a
physical slope, map distance or normal-vector angle. Non-square cells therefore
still use a grid-neighbour formulation. Larger contrasts allow more smoothing
across height differences. Sharp banks and noise can both be retained if the
contrast is too small; this filter is not a replacement for spike repair.

A positive change cap bounds the final contour surface relative to its input
height before rounding/filtering. At average factor 1, that input is the edited
DTM. For averaging, the cap is relative to the block height, and the report warns
that it does not bound the change from every original source cell. The cap is a
surface-height bound, not a bound on horizontal contour displacement, slope,
drainage topology or survey error.

Protected samples remain exact. The filter uses two float32 working grids and
local four-neighbour operations, rather than a radius-dependent nested window.
Incremental support expands by the number of diffusion passes, plus support for
any other enabled filters. Tests compare local updates against full regeneration.

## Drawn breaklines and waterbodies

- **Preserve breakline:** locks existing valid DTM heights in a 1–5 source-pixel
  radius along a drawn polyline. It protects an existing bank, ridge or channel
  during contour-surface treatment; it does not infer where the true feature is.
- **Graded breakline:** assigns endpoint-interpolated heights along that corridor.
  Interpolation follows segment lengths in the source map coordinate system, or
  source pixels if unreferenced. Geographic coordinate lengths are not geodesic.
- **Downstream grade:** uses the same interpolation and requires end height no
  greater than start height. It enforces that local drawn profile, not a connected
  stream network, basin drainage, flow accumulation or a complete river surface.
- **Locked waterbody:** assigns the entered known level inside a drawn polygon,
  excluding up to 16 island rings. NoData remains NoData unless filling is chosen.
  It protects the level and clips derived contour geometry to the exact open
  water interior. Contours on islands and boundary-coincident pieces remain.
  A water level equal to a contour is retained rather than shifted.

A breakline is a **raster corridor**, not a zero-width constrained TIN edge or
an exact surveyed 3D line. Its fixed samples form a Dirichlet boundary for the
four-neighbour filter; tests verify that changes on one side cannot propagate
through a continuous protected corridor. Ordinary box/regional means can mix
across it and are consequently disabled while these features are active.
Averaging must remain 1. Conflicting terrain edits and differing overlapping
constraints are rejected before any change is committed.

Water clipping prevents water-interior linework for the supplied polygons. It
does not route a contour along the shoreline, adjust a surveyed water level,
straighten river crossings or repair land below a lake's known level. QA flags
valid adjacent land more than 0.1 contour interval below that level. The 0.1
threshold is an application diagnostic, not a criterion copied from the USGS
report. Review the boundary, elevation and banks when flagged.

Waterbody rasterization uses scanline spans, avoiding a per-cell test against
every vertex of every island. Detailed drawings support requests up to 512 KiB
within the stated ring/vertex and cell limits.

Add/remove operations support Undo/Redo and invalidate the current contour
revision. Removing protection retains edited DTM heights. Features are session
state; save the DTM and export GeoPackage before closing. GeoPackage includes
`terrain_breaklines` and `terrain_waterbodies` as source-CRS vector layers, with
endpoint/level attributes and island geometry, plus a JSON processing audit.
Preserve-mode breaklines have NULL endpoint attributes because their heights
come from the saved DTM. Reopening a TIFF or GeoPackage does not restore an app
constraint session; import/project restoration remains future work.

## Validation and remaining work

The synthetic regressions demonstrate plateau-noise contour reduction without
size filtering, sharp-bank retention, a strict change cap within float32 output,
protected samples, barrier isolation, island preservation, atomic conflict
rejection, history, incremental/full equivalence and water-interior exclusion.
Independent GDAL/Fiona/Shapely/Rasterio checks validate exported geometry and
saved terrain. These are implementation tests, not proof of improved surveyed
accuracy or performance on every terrain type.

Still missing are a full normal-vector terrain filter, topology-constrained
vector simplify/smooth/simplify, automatic hydrographic ingestion, drainage-network
conditioning, full river grading, depression classification, buffered AOI
processing and the report's hydrographic discrepancy QA. Representative project
DTM, mapped hydrography, spot heights and independent control are needed to assess
finished topographic contours at the intended interval and map scale.
