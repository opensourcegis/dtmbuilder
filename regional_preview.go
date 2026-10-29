package main

import (
	"image"
	"image/color"
	"math"
)

// Keep the original image extent so regional contours and the averaged surface
// stay aligned, including transparent cropped right/bottom edge blocks.
func previewContourSurface(source *Grid, c *ContourSet) image.Image {
	scale := math.Max(1, math.Max(float64(source.W), float64(source.H))/1000)
	w, h := max(1, int(float64(source.W)/scale)), max(1, int(float64(source.H)/scale))
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	g := c.Surface
	basis := hillshadeBasis(g)
	f := c.Params.AverageFactor
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			xx, yy := int(float64(x)*scale)/f, int(float64(y)*scale)/f
			if xx >= g.W || yy >= g.H || !valid(g.Z[yy*g.W+xx]) {
				continue
			}
			shade := hillshadeAt(g, g.Z, xx, yy, basis)
			im.SetNRGBA(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	return im
}
