package render

import (
	"image"
	"image/color"
	"math"

	"github.com/tazhate/serious-animator/internal/ease"
	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/raster"
	"github.com/tazhate/serious-animator/internal/svgscene"
)

// item is a scene element prepared once: geometry roughened in viewBox
// units, so it can be drawn under any camera or motion transform.
type item struct {
	src *svgscene.Item

	// Stroke: the main roughened pass and a second, offset pass that gives
	// the doubled line of a sketch.
	lines     []line
	lines2    []line
	strokeLen float64
	width     float64
	stroke    color.NRGBA
	taper     bool

	// Fill: outline polygons, plus hatch lines when the fill is hachured.
	fillPolys [][]geom.Pt
	fill      color.NRGBA
	hatch     []line
	hatchLen  float64
	hatchW    float64
	solid     bool
	sweep     sweep
	wash      sweep // reveals the hatch underlay row by row with the lines

	text    bool
	anim    string  // "draw", "pop" or "fade"
	pivot   geom.Pt // pop scales around this point
	popSFX  bool    // explicit pops make a sound, automatic arrowheads do not
	hl      bool    // synthetic highlight: fades after its beat
	bounds  geom.Rect
	natural float64 // seconds at the default drawing speed
	motion  *motion

	startTip, endTip geom.Pt
	hasTip           bool
}

type line struct {
	pts    []geom.Pt
	cum    []float64
	closed bool
}

func (l line) length() float64 { return l.cum[len(l.cum)-1] }

func newLine(pts []geom.Pt, closed bool) line {
	return line{pts: pts, cum: geom.Cumulative(pts), closed: closed}
}

// sweep reveals a solid shape along a direction: points whose projection on
// dir is below the moving front are visible, with a soft edge of width
// feather. Everything is in viewBox units.
type sweep struct {
	dir        geom.Pt
	lo, hi     float64 // projection range over the shape
	feather    float64
	center     geom.Pt
	halfAcross float64   // half extent across dir, for the marker's zigzag
	stops      []float64 // text: glyph boundaries, written one after another
}

func newSweep(box geom.Rect, dir geom.Pt, feather float64) sweep {
	corners := []geom.Pt{box.Min, {X: box.Max.X, Y: box.Min.Y}, {X: box.Min.X, Y: box.Max.Y}, box.Max}
	perp := geom.Pt{X: -dir.Y, Y: dir.X}
	s := sweep{dir: dir, lo: math.Inf(1), hi: math.Inf(-1), feather: feather}
	alo, ahi := math.Inf(1), math.Inf(-1)
	for _, c := range corners {
		pr := c.X*dir.X + c.Y*dir.Y
		s.lo, s.hi = min(s.lo, pr), max(s.hi, pr)
		ac := c.X*perp.X + c.Y*perp.Y
		alo, ahi = min(alo, ac), max(ahi, ac)
	}
	s.center = box.Min.Lerp(box.Max, 0.5)
	s.halfAcross = (ahi - alo) / 2
	return s
}

// front is the position of the reveal front at progress u. Text advances
// glyph by glyph, easing within each glyph like a hand writing letters.
func (s sweep) front(u float64) float64 {
	u = ease.Clamp01(u)
	if n := len(s.stops) - 1; n > 0 {
		x := u * float64(n)
		g := min(int(x), n-1)
		f := ease.Smoothstep(x - float64(g))
		a, b := s.stops[g], s.stops[g+1]
		return a + (b-a)*f + s.feather*u
	}
	return s.lo + (s.hi-s.lo+s.feather)*u
}

// cut returns the per-pixel coverage of the reveal at progress u for a
// frame drawn through m, a similarity transform from viewBox to pixels.
func (s sweep) cut(u float64, m geom.Mat) func(x, y int) float64 {
	sc := m.Scale()
	d := geom.Pt{X: m[0]*s.dir.X + m[2]*s.dir.Y, Y: m[1]*s.dir.X + m[3]*s.dir.Y}.Mul(1 / sc)
	origin := m[4]*d.X + m[5]*d.Y
	c := s.front(u)
	return func(x, y int) float64 {
		pr := ((float64(x)+0.5)*d.X + (float64(y)+0.5)*d.Y - origin) / sc
		return min(max((c-pr)/s.feather, 0), 1)
	}
}

// tip places the marker on the reveal front, zigzagging across it the way a
// hand hatches a fill.
func (s sweep) tip(u, cycles float64) geom.Pt {
	c := min(s.front(u), s.hi)
	mid := s.center.X*s.dir.X + s.center.Y*s.dir.Y
	perp := geom.Pt{X: -s.dir.Y, Y: s.dir.X}
	across := s.halfAcross * 0.8 * math.Sin(u*cycles*2*math.Pi)
	return s.center.Add(s.dir.Mul(c - mid)).Add(perp.Mul(across))
}

// Drawing constants. Speeds are in pixels of a 720p frame showing the
// reference view, so they do not depend on the output resolution.
const (
	strokeSpeed   = 650.0  // px of stroke per second
	hatchSpeed    = 2800.0 // px of hatch line per second: a quick scribble
	fillPerPx     = 1.0 / 420
	fillBase      = 0.25
	hatchBase     = 0.2
	textPerRune   = 0.075
	textBase      = 0.2
	popTime       = 0.35
	fadeTime      = 0.4
	minItemTime   = 0.2
	maxItemTime   = 4.0
	strokeShare   = 0.6 // of an outlined fill's time spent on the outline
	pass2Lag      = 0.08
	flattenTol    = 0.08 // viewBox units
	roughAmpPx    = 1.3
	roughPeriodPx = 90.0
	hatchAngle    = -41.0
	underlayAlpha = 0.28
	minWidthPx    = 0.8
)

// prepare builds an item in viewBox units. unit is how many 720p pixels one
// viewBox unit covers in the reference view.
func prepare(it *svgscene.Item, unit, roughness float64, seed uint64) *item {
	pi := &item{src: it, text: it.IsText(), anim: it.Anim}
	if pi.anim == "" {
		pi.anim = "draw"
	}
	polys := it.Path.Flatten(flattenTol)
	amp, period := roughAmpPx/unit*roughness, roughPeriodPx/unit
	rough := !pi.text && roughness > 0
	pts := make([][]geom.Pt, len(polys))
	for i, pl := range polys {
		if rough {
			pl = geom.Roughen(pl, amp, period, seed+uint64(i))
		}
		pts[i] = pl.Pts
	}
	pi.bounds = it.Bounds()

	if it.HasStroke {
		pi.width = max(it.StrokeWidth, minWidthPx/unit)
		pi.stroke = it.Stroke
		pi.taper = !pi.text
		for i, pl := range pts {
			l := newLine(pl, polys[i].Closed)
			pi.lines = append(pi.lines, l)
			pi.strokeLen += l.length()
			if rough {
				p2 := geom.Roughen(polys[i], amp*1.6, period*1.3, seed^0x9E37+uint64(i)*7)
				pi.lines2 = append(pi.lines2, newLine(p2.Pts, polys[i].Closed))
			}
		}
	}

	if it.HasFill {
		pi.fillPolys = pts
		pi.fill = it.Fill
		ref := 4.0
		if it.HasStroke {
			ref = it.StrokeWidth
		}
		gap := max(4*ref, 14)
		pi.solid = solidFill(it, gap)
		if !pi.solid {
			pi.hatchW = max(0.55*ref, 2.2)
			for i, h := range geom.Hachure(pts, hatchAngle, gap) {
				pl := geom.Polyline{Pts: h}
				if rough {
					pl = geom.Roughen(pl, amp*0.6, period, seed^0x51ED+uint64(i))
				}
				l := newLine(pl.Pts, false)
				pi.hatch = append(pi.hatch, l)
				pi.hatchLen += l.length()
			}
			if len(pi.hatch) == 0 {
				pi.solid = true
			}
			// Hatch rows are laid down across the line direction.
			sn, cs := math.Sincos(hatchAngle * math.Pi / 180)
			pi.wash = newSweep(pi.bounds, geom.Pt{X: -sn, Y: cs}, gap)
		}
	}

	if pi.text || (it.HasFill && pi.solid) {
		dir := geom.Pt{X: math.Cos(20 * math.Pi / 180), Y: math.Sin(20 * math.Pi / 180)}
		feather := 10 / unit
		if pi.text {
			dir = it.TextDir
			if dir == (geom.Pt{}) {
				dir = geom.Pt{X: 1}
			}
			feather = 6 / unit
		}
		pi.sweep = newSweep(pi.bounds, dir, feather)
		if pi.text {
			pi.sweep.stops = it.Glyphs
		}
	}

	pi.pivot = pi.bounds.Min.Lerp(pi.bounds.Max, 0.5)
	pi.natural = pi.estimate(unit)
	pi.tips()
	return pi
}

// solidFill decides between a flat fill and hatching. Text, dark ink, white
// knockouts, translucent and small shapes stay flat: hatching would read as
// noise there.
func solidFill(it *svgscene.Item, gap float64) bool {
	switch it.FillStyle {
	case "solid":
		return true
	case "hachure":
		return false
	}
	if it.IsText() || it.Fill.A < 250 {
		return true
	}
	lum := (0.2126*float64(it.Fill.R) + 0.7152*float64(it.Fill.G) + 0.0722*float64(it.Fill.B)) / 255
	if lum < 0.3 || lum > 0.96 {
		return true
	}
	b := it.Path.Bounds()
	return min(b.W(), b.H()) < 2.5*gap
}

func (pi *item) estimate(unit float64) float64 {
	var t float64
	switch {
	case pi.anim == "pop":
		return popTime
	case pi.anim == "fade":
		return fadeTime
	case pi.text:
		t = textBase + textPerRune*float64(len([]rune(pi.src.Text)))
	default:
		if pi.strokeLen > 0 {
			t += pi.strokeLen * unit / strokeSpeed
		}
		switch {
		case pi.hatchLen > 0:
			t += hatchBase + pi.hatchLen*unit/hatchSpeed
		case pi.fillPolys != nil:
			t += fillBase + math.Sqrt(max(pi.bounds.Area(), 0))*unit*fillPerPx
		}
	}
	return min(max(t, minItemTime), maxItemTime)
}

// tips records where the marker starts and ends this item, for its travel
// between items.
func (pi *item) tips() {
	if pi.anim != "draw" {
		return
	}
	switch {
	case pi.text, pi.fillPolys != nil && pi.solid && len(pi.lines) == 0:
		pi.startTip, pi.endTip = pi.sweep.tip(0, pi.cycles()), pi.sweep.tip(1, pi.cycles())
	case len(pi.lines) > 0:
		pi.startTip = pi.lines[0].pts[0]
		last := pi.lines[len(pi.lines)-1]
		pi.endTip = last.pts[len(last.pts)-1]
		if pi.fillPolys != nil {
			if pi.solid {
				pi.endTip = pi.sweep.tip(1, pi.cycles())
			} else {
				h := pi.hatch[len(pi.hatch)-1]
				pi.endTip = h.pts[len(h.pts)-1]
			}
		}
	case len(pi.hatch) > 0:
		pi.startTip = pi.hatch[0].pts[0]
		h := pi.hatch[len(pi.hatch)-1]
		pi.endTip = h.pts[len(h.pts)-1]
	default:
		return
	}
	pi.hasTip = true
}

// cycles is how many times the marker crosses the sweep front: text gets a
// gentle bob, larger fills get real hatching.
func (pi *item) cycles() float64 {
	if pi.text {
		return max(2, float64(len([]rune(pi.src.Text)))/2)
	}
	return max(2, (pi.sweep.hi-pi.sweep.lo)/50)
}

func withAlpha(c color.NRGBA, a float64) color.NRGBA {
	c.A = uint8(float64(c.A)*min(max(a, 0), 1) + 0.5)
	return c
}

func xf(m geom.Mat, pts []geom.Pt) []geom.Pt {
	out := make([]geom.Pt, len(pts))
	for i, p := range pts {
		out[i] = m.Apply(p)
	}
	return out
}

func xfAll(m geom.Mat, polys [][]geom.Pt) [][]geom.Pt {
	out := make([][]geom.Pt, len(polys))
	for i, p := range polys {
		out[i] = xf(m, p)
	}
	return out
}

// pressure is the marker's width profile along an open line: it lands
// lightly, presses through the middle and lifts a little at the end.
func pressure(s, total float64) float64 {
	if total <= 0 {
		return 1
	}
	ramp := min(total*0.2, 40)
	in := 0.55 + 0.45*ease.Smoothstep(s/ramp)
	out := 0.7 + 0.3*ease.Smoothstep((total-s)/ramp)
	return min(in, out)
}

// strokeLines paints the first upto units of lines through m and returns the
// pen position in viewBox units.
func strokeLines(p *raster.Painter, dst *image.RGBA, m geom.Mat, lines []line, upto, width float64, taper bool, c color.NRGBA) (geom.Pt, bool) {
	scale := m.Scale()
	w := max(width*scale, minWidthPx)
	var (
		polys [][]geom.Pt
		tip   geom.Pt
		ok    bool
	)
	for _, l := range lines {
		if upto <= 0 {
			break
		}
		total := l.length()
		part := geom.Prefix(l.pts, l.cum, upto)
		if len(part) > 0 {
			dev := xf(m, part)
			if taper && !l.closed {
				ws := make([]float64, len(part))
				for i := range part {
					s := min(upto, total)
					if i < len(part)-1 {
						s = l.cum[i]
					}
					ws[i] = max(w*pressure(s, total), minWidthPx)
				}
				polys = raster.TaperedStrokePolys(polys, dev, ws)
			} else {
				polys = raster.StrokePolys(polys, dev, w)
			}
			tip, ok = part[len(part)-1], true
		}
		upto -= total
	}
	if len(polys) > 0 {
		p.Fill(dst, polys, c)
	}
	return tip, ok
}

// sweepFill reveals polygons through a mask and the sweep at progress u.
func sweepFill(p *raster.Painter, dst *image.RGBA, m geom.Mat, polys [][]geom.Pt, s sweep, u float64, c color.NRGBA) {
	dev := xfAll(m, polys)
	b, ok := maskBounds(dev, dst.Bounds())
	if !ok {
		return
	}
	var cov func(x, y int) float64
	if u < 1 {
		cov = s.cut(u, m)
	}
	raster.Composite(dst, p.Mask(dev, b), c, cov)
}

func maskBounds(polys [][]geom.Pt, clip image.Rectangle) (image.Rectangle, bool) {
	r := geom.EmptyRect()
	for _, p := range polys {
		for _, q := range p {
			r = r.Extend(q)
		}
	}
	if r.Empty() {
		return image.Rectangle{}, false
	}
	b := image.Rect(int(math.Floor(r.Min.X)), int(math.Floor(r.Min.Y)), int(math.Ceil(r.Max.X))+1, int(math.Ceil(r.Max.Y))+1).Intersect(clip)
	return b, !b.Empty()
}

// textPolys returns the glyph outlines and, for outlined text, the stroke
// outline, both in device space.
func (pi *item) textPolys(m geom.Mat) (fill, stroke [][]geom.Pt) {
	if pi.fillPolys != nil {
		fill = xfAll(m, pi.fillPolys)
	}
	if len(pi.lines) > 0 {
		w := max(pi.width*m.Scale(), minWidthPx)
		for _, l := range pi.lines {
			stroke = raster.StrokePolys(stroke, xf(m, l.pts), w)
		}
	}
	return fill, stroke
}

// drawFull paints the finished item: fill under stroke, as SVG does.
func (pi *item) drawFull(p *raster.Painter, dst *image.RGBA, m geom.Mat, alpha float64) {
	if alpha <= 0 {
		return
	}
	if pi.text {
		fill, stroke := pi.textPolys(m)
		if fill != nil {
			p.Fill(dst, fill, withAlpha(pi.fill, alpha))
		}
		if stroke != nil {
			p.Fill(dst, stroke, withAlpha(pi.stroke, alpha))
		}
		return
	}
	pi.drawFill(p, dst, m, alpha, 1)
	pi.drawStroke(p, dst, m, alpha, 1)
}

func (pi *item) drawFill(p *raster.Painter, dst *image.RGBA, m geom.Mat, alpha, u float64) (geom.Pt, bool) {
	switch {
	case pi.fillPolys == nil || u <= 0:
		return geom.Pt{}, false
	case pi.solid:
		if u >= 1 {
			p.Fill(dst, xfAll(m, pi.fillPolys), withAlpha(pi.fill, alpha))
			return geom.Pt{}, false
		}
		sweepFill(p, dst, m, pi.fillPolys, pi.sweep, u, withAlpha(pi.fill, alpha))
		return pi.sweep.tip(u, pi.cycles()), true
	}
	// Hachure: a pale wash of the fill color under quick scribbled lines.
	wash := withAlpha(pi.fill, alpha*underlayAlpha)
	if u >= 1 {
		p.Fill(dst, xfAll(m, pi.fillPolys), wash)
	} else {
		sweepFill(p, dst, m, pi.fillPolys, pi.wash, u, wash)
	}
	return strokeLines(p, dst, m, pi.hatch, pi.hatchLen*u, pi.hatchW, false, withAlpha(pi.fill, alpha))
}

func (pi *item) drawStroke(p *raster.Painter, dst *image.RGBA, m geom.Mat, alpha, u float64) (geom.Pt, bool) {
	if len(pi.lines) == 0 || u <= 0 {
		return geom.Pt{}, false
	}
	c := withAlpha(pi.stroke, alpha)
	if u2 := (u - pass2Lag) / (1 - pass2Lag); len(pi.lines2) > 0 && u2 > 0 {
		var l2 float64
		for _, l := range pi.lines2 {
			l2 += l.length()
		}
		strokeLines(p, dst, m, pi.lines2, l2*min(u2, 1), pi.width*0.55, pi.taper, withAlpha(pi.stroke, alpha*0.9))
	}
	return strokeLines(p, dst, m, pi.lines, pi.strokeLen*u, pi.width, pi.taper, c)
}

// drawPartial paints the item at progress u in (0, 1) and returns where the
// marker nib is, in viewBox units, and which ink it carries.
func (pi *item) drawPartial(p *raster.Painter, dst *image.RGBA, m geom.Mat, alpha, u float64) (geom.Pt, color.NRGBA, bool) {
	if pi.text {
		fill, stroke := pi.textPolys(m)
		cov := pi.sweep.cut(u, m)
		ink := pi.fill
		all := append(fill, stroke...)
		if b, ok := maskBounds(all, dst.Bounds()); ok {
			if fill != nil {
				raster.Composite(dst, p.Mask(fill, b), withAlpha(pi.fill, alpha), cov)
			}
			if stroke != nil {
				raster.Composite(dst, p.Mask(stroke, b), withAlpha(pi.stroke, alpha), cov)
				if fill == nil {
					ink = pi.stroke
				}
			}
		}
		return pi.sweep.tip(u, pi.cycles()), ink, true
	}
	if len(pi.lines) == 0 {
		tip, ok := pi.drawFill(p, dst, m, alpha, u)
		return tip, pi.fill, ok
	}
	if pi.fillPolys == nil {
		tip, ok := pi.drawStroke(p, dst, m, alpha, ease.InOutSine(u))
		return tip, pi.stroke, ok
	}
	// Outline first, then the fill comes in underneath it.
	if u < strokeShare {
		tip, ok := pi.drawStroke(p, dst, m, alpha, ease.InOutSine(u/strokeShare))
		return tip, pi.stroke, ok
	}
	tip, ok := pi.drawFill(p, dst, m, alpha, (u-strokeShare)/(1-strokeShare))
	pi.drawStroke(p, dst, m, alpha, 1)
	return tip, pi.fill, ok
}
