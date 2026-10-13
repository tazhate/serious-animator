// Package raster draws flattened geometry onto RGBA frames: round-capped
// strokes, nonzero fills, alpha masks and the marker sprite.
package raster

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	"github.com/tazhate/serious-animator/internal/geom"
)

// Painter rasterizes shapes. It reuses one scratch rasterizer, so a Painter
// must not be shared between goroutines.
type Painter struct {
	z vector.Rasterizer
}

// polyBounds returns the integer pixel box of a set of polygons, clipped to
// clip. ok is false when nothing is visible.
func polyBounds(polys [][]geom.Pt, clip image.Rectangle) (image.Rectangle, bool) {
	r := geom.EmptyRect()
	for _, p := range polys {
		for _, q := range p {
			r = r.Extend(q)
		}
	}
	if r.Empty() {
		return image.Rectangle{}, false
	}
	b := image.Rect(
		int(math.Floor(r.Min.X)), int(math.Floor(r.Min.Y)),
		int(math.Ceil(r.Max.X))+1, int(math.Ceil(r.Max.Y))+1,
	).Intersect(clip)
	return b, !b.Empty()
}

// Fill paints polygons with the nonzero rule. Polygons are closed implicitly.
func (p *Painter) Fill(dst draw.Image, polys [][]geom.Pt, c color.Color) {
	p.fillOp(dst, polys, image.NewUniform(c), draw.Over)
}

// Mask rasterizes polygons into an alpha mask covering box.
func (p *Painter) Mask(polys [][]geom.Pt, box image.Rectangle) *image.Alpha {
	m := image.NewAlpha(box)
	p.fillOp(m, polys, image.Opaque, draw.Src)
	return m
}

func (p *Painter) fillOp(dst draw.Image, polys [][]geom.Pt, src image.Image, op draw.Op) {
	b, ok := polyBounds(polys, dst.Bounds())
	if !ok {
		return
	}
	p.z.Reset(b.Dx(), b.Dy())
	p.z.DrawOp = op
	ox, oy := float64(b.Min.X), float64(b.Min.Y)
	for _, poly := range polys {
		if len(poly) < 3 {
			continue
		}
		p.z.MoveTo(float32(poly[0].X-ox), float32(poly[0].Y-oy))
		for _, q := range poly[1:] {
			p.z.LineTo(float32(q.X-ox), float32(q.Y-oy))
		}
		p.z.ClosePath()
	}
	p.z.Draw(dst, b, src, image.Point{})
}

// StrokePolys builds the outline of a round-capped, round-joined stroke as a
// set of convex pieces: one quad per segment and a disc at every end and
// every sharp-enough corner. All pieces wind the same way, so the nonzero
// rule unions them without holes where they overlap.
func StrokePolys(dst [][]geom.Pt, pts []geom.Pt, width float64) [][]geom.Pt {
	if len(pts) == 0 || width <= 0 {
		return dst
	}
	r := width / 2
	if len(pts) == 1 {
		return append(dst, disc(pts[0], r))
	}
	dst = append(dst, disc(pts[0], r))
	var prevDir geom.Pt
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		d := b.Sub(a)
		l := d.Len()
		if l < 1e-9 {
			continue
		}
		u := d.Mul(1 / l)
		n := geom.Pt{X: -u.Y * r, Y: u.X * r}
		dst = append(dst, ccw([]geom.Pt{a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)}))
		if prevDir != (geom.Pt{}) {
			dst = joint(dst, a, prevDir, u, r, r)
		}
		prevDir = u
	}
	return append(dst, disc(pts[len(pts)-1], r))
}

// joint closes the wedge-shaped gap a turn leaves between two segment quads:
// a disc for sharp turns, two thin triangles for gentle ones. ri is the
// incoming segment's half-width at the joint, ro the outgoing one's.
func joint(dst [][]geom.Pt, a, in, out geom.Pt, ri, ro float64) [][]geom.Pt {
	dot := in.X*out.X + in.Y*out.Y
	if dot < math.Cos(30*math.Pi/180) {
		return append(dst, disc(a, max(ri, ro)))
	}
	if r := max(ri, ro); r*math.Sqrt(max(2-2*dot, 0)) < 0.05 {
		return dst // the gap is far below a pixel
	}
	ni := geom.Pt{X: -in.Y, Y: in.X}
	no := geom.Pt{X: -out.Y, Y: out.X}
	return append(dst,
		ccw([]geom.Pt{a, a.Add(ni.Mul(ri)), a.Add(no.Mul(ro))}),
		ccw([]geom.Pt{a, a.Sub(ni.Mul(ri)), a.Sub(no.Mul(ro))}))
}

// disc approximates a circle with enough sides to look round at radius r.
func disc(c geom.Pt, r float64) []geom.Pt {
	n := min(max(int(math.Ceil(r*1.5)), 8), 48)
	pts := make([]geom.Pt, n)
	for i := range n {
		s, co := math.Sincos(2 * math.Pi * float64(i) / float64(n))
		pts[i] = geom.Pt{X: c.X + r*co, Y: c.Y + r*s}
	}
	return pts
}

// ccw returns the polygon wound counter-clockwise (positive area in y-down).
func ccw(poly []geom.Pt) []geom.Pt {
	area := 0.0
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		area += a.X*b.Y - b.X*a.Y
	}
	if area < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
	return poly
}

// TaperedStrokePolys is StrokePolys with a width that changes along the
// line: widths[i] is the full width at pts[i]. Segments become trapezoids,
// and every end and sharp joint gets a disc of the local width.
func TaperedStrokePolys(dst [][]geom.Pt, pts []geom.Pt, widths []float64) [][]geom.Pt {
	if len(pts) == 0 {
		return dst
	}
	if len(pts) == 1 {
		return append(dst, disc(pts[0], widths[0]/2))
	}
	dst = append(dst, disc(pts[0], widths[0]/2))
	var prevDir geom.Pt
	prevW := widths[0]
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		d := b.Sub(a)
		l := d.Len()
		if l < 1e-9 {
			continue
		}
		u := d.Mul(1 / l)
		n := geom.Pt{X: -u.Y, Y: u.X}
		na, nb := n.Mul(widths[i-1]/2), n.Mul(widths[i]/2)
		dst = append(dst, ccw([]geom.Pt{a.Add(na), b.Add(nb), b.Sub(nb), a.Sub(na)}))
		if prevDir != (geom.Pt{}) {
			dst = joint(dst, a, prevDir, u, prevW/2, widths[i-1]/2)
		}
		prevDir, prevW = u, widths[i]
	}
	return append(dst, disc(pts[len(pts)-1], widths[len(pts)-1]/2))
}

// Stroke paints a polyline with the given width.
func (p *Painter) Stroke(dst draw.Image, pts []geom.Pt, width float64, c color.Color) {
	p.Fill(dst, StrokePolys(nil, pts, width), c)
}

// Composite blends a solid color through mask*cov onto dst, where cov
// returns a per-pixel coverage in [0, 1]. A nil cov means full coverage.
func Composite(dst *image.RGBA, mask *image.Alpha, c color.NRGBA, cov func(x, y int) float64) {
	b := mask.Bounds().Intersect(dst.Bounds())
	ca := float64(c.A) / 255
	cr, cg, cb := float64(c.R), float64(c.G), float64(c.B)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		mi := mask.PixOffset(b.Min.X, y)
		di := dst.PixOffset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x, mi, di = x+1, mi+1, di+4 {
			m := mask.Pix[mi]
			if m == 0 {
				continue
			}
			a := float64(m) / 255 * ca
			if cov != nil {
				a *= cov(x, y)
				if a <= 0 {
					continue
				}
			}
			px := dst.Pix[di : di+4 : di+4]
			px[0] = uint8(float64(px[0])*(1-a) + cr*a + 0.5)
			px[1] = uint8(float64(px[1])*(1-a) + cg*a + 0.5)
			px[2] = uint8(float64(px[2])*(1-a) + cb*a + 0.5)
			px[3] = uint8(float64(px[3])*(1-a) + 255*a + 0.5)
		}
	}
}

// Clear fills the image with opaque white.
func Clear(img *image.RGBA) {
	for i := range img.Pix {
		img.Pix[i] = 255
	}
}
