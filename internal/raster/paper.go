package raster

import (
	"image"
	"image/color"
	"math"

	"github.com/tazhate/serious-animator/internal/geom"
)

// PaperColor is the warm off-white of the drawing board.
var PaperColor = color.NRGBA{0xfb, 0xf8, 0xf1, 0xff}

// Paper paints the board background: the paper color with a faint static
// grain, soft low-frequency blotches and a slight vignette. The texture is
// tied to the screen, not the scene, so it stays put under camera moves.
func Paper(w, h int, seed uint64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	far := cx*cx + cy*cy
	cell := float64(max(h, 1)) / 6 // blotch size
	for y := range h {
		for x := range w {
			grain := unit(geom.Hash64(seed^uint64(y*w+x))) - 0.5
			blotch := smoothNoise2(float64(x)/cell, float64(y)/cell, seed)
			dx, dy := float64(x)-cx, float64(y)-cy
			k := 1 + 0.018*grain + 0.012*blotch - 0.05*(dx*dx+dy*dy)/far
			i := img.PixOffset(x, y)
			img.Pix[i+0] = clamp8(float64(PaperColor.R) * k)
			img.Pix[i+1] = clamp8(float64(PaperColor.G) * k)
			img.Pix[i+2] = clamp8(float64(PaperColor.B) * k)
			img.Pix[i+3] = 0xff
		}
	}
	return img
}

func unit(h uint64) float64 { return float64(h>>11) / float64(1<<53) }

func clamp8(v float64) uint8 { return uint8(min(max(v+0.5, 0), 255)) }

// smoothNoise2 is bilinear value noise in [-1, 1] on an integer lattice.
func smoothNoise2(x, y float64, seed uint64) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	at := func(i, j float64) float64 {
		return unit(geom.Hash64(seed^uint64(int64(i))*0x9E3779B97F4A7C15^uint64(int64(j))*0xC2B2AE3D27D4EB4F))*2 - 1
	}
	a := at(ix, iy) + (at(ix+1, iy)-at(ix, iy))*fx
	b := at(ix, iy+1) + (at(ix+1, iy+1)-at(ix, iy+1))*fx
	return a + (b-a)*fy
}

// FadeTo blends img toward bg by k in [0, 1]. Both images have the same size.
func FadeTo(img, bg *image.RGBA, k float64) {
	if k <= 0 {
		return
	}
	k = min(k, 1)
	for i, v := range img.Pix {
		img.Pix[i] = uint8(float64(v) + (float64(bg.Pix[i])-float64(v))*k + 0.5)
	}
}
