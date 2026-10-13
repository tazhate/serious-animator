package raster

import (
	"image/color"
	"image/draw"
	"math"

	"github.com/tazhate/serious-animator/internal/geom"
)

var (
	markerDark   = color.NRGBA{0x33, 0x33, 0x33, 0xff}
	markerBody   = color.NRGBA{0xf4, 0xf4, 0xf0, 0xff}
	markerShadow = color.NRGBA{0x20, 0x18, 0x10, 0x38}
	markerAngle  = 50 * math.Pi / 180 // body leans toward the lower right, like a right hand
)

// Marker draws a whiteboard marker whose nib touches tip. ink colors the nib
// and the cap band; scale is 1 at 720p. lift in [0, 1] raises the marker off
// the board between strokes: the nib floats above tip and casts a shadow.
func (p *Painter) Marker(dst draw.Image, tip geom.Pt, ink color.NRGBA, scale, lift float64) {
	ink.A = 255
	s, c := math.Sincos(markerAngle)
	d := geom.Pt{X: c, Y: s}
	n := geom.Pt{X: -s, Y: c}
	lift = min(max(lift, 0), 1)
	body := func(at geom.Pt, fill func(poly []geom.Pt, c color.NRGBA)) {
		// band is the quad between distances a and b along the body with
		// half-widths wa and wb.
		band := func(a, b, wa, wb float64) []geom.Pt {
			pa := at.Add(d.Mul(a * scale))
			pb := at.Add(d.Mul(b * scale))
			return ccw([]geom.Pt{
				pa.Add(n.Mul(wa * scale)), pb.Add(n.Mul(wb * scale)),
				pb.Sub(n.Mul(wb * scale)), pa.Sub(n.Mul(wa * scale)),
			})
		}
		const outline = 2.0
		fill(band(0, 20, 1.5, 6), ink)
		fill(band(18, 34, 8, 10), markerDark)
		fill(band(32, 170, 13, 13), markerDark)
		fill(band(34, 168-outline, 13-outline, 13-outline), markerBody)
		fill(band(140, 168-outline, 13-outline, 13-outline), ink)
	}
	if lift > 0 {
		sh := markerShadow
		sh.A = uint8(float64(sh.A) * lift)
		var polys [][]geom.Pt
		body(tip.Add(geom.Pt{X: 7, Y: 9}.Mul(lift*scale)), func(poly []geom.Pt, _ color.NRGBA) { polys = append(polys, poly) })
		p.Fill(dst, polys, sh)
		tip = tip.Add(geom.Pt{X: -3, Y: -8}.Mul(lift * scale))
	}
	body(tip, func(poly []geom.Pt, c color.NRGBA) { p.Fill(dst, [][]geom.Pt{poly}, c) })
}
