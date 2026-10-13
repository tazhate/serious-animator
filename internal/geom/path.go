package geom

import (
	"fmt"
	"math"
	"strconv"
)

// SegKind tells a straight segment from a cubic Bézier.
type SegKind uint8

const (
	Line SegKind = iota
	Cubic
)

// Seg ends at P. For cubics C1 and C2 are the control points.
type Seg struct {
	Kind   SegKind
	C1, C2 Pt
	P      Pt
}

// Sub is one subpath: a start point followed by segments.
type Sub struct {
	Start  Pt
	Segs   []Seg
	Closed bool
}

// Path is a list of subpaths. Quadratics and arcs are stored as cubics, so a
// Path survives any affine transform exactly.
type Path []Sub

// Transform maps every point of the path through m.
func (p Path) Transform(m Mat) Path {
	out := make(Path, len(p))
	for i, s := range p {
		ns := Sub{Start: m.Apply(s.Start), Closed: s.Closed, Segs: make([]Seg, len(s.Segs))}
		for j, g := range s.Segs {
			ns.Segs[j] = Seg{Kind: g.Kind, C1: m.Apply(g.C1), C2: m.Apply(g.C2), P: m.Apply(g.P)}
		}
		out[i] = ns
	}
	return out
}

// Bounds returns the bounding box of all points, control points included.
// That over-estimates curves a little, which is fine for layout checks.
func (p Path) Bounds() Rect {
	r := EmptyRect()
	for _, s := range p {
		r = r.Extend(s.Start)
		for _, g := range s.Segs {
			if g.Kind == Cubic {
				r = r.Extend(g.C1).Extend(g.C2)
			}
			r = r.Extend(g.P)
		}
	}
	return r
}

// Polyline is a flattened subpath. A closed polyline repeats its first point
// at the end, so its arc length includes the closing edge.
type Polyline struct {
	Pts    []Pt
	Closed bool
}

// Flatten converts curves to polylines whose chords stay within tol of the
// true curve. tol is in the path's own units.
func (p Path) Flatten(tol float64) []Polyline {
	tol = max(tol, 1e-3)
	var out []Polyline
	for _, s := range p {
		pts := []Pt{s.Start}
		cur := s.Start
		for _, g := range s.Segs {
			switch g.Kind {
			case Line:
				pts = append(pts, g.P)
			case Cubic:
				pts = flattenCubic(pts, cur, g.C1, g.C2, g.P, tol)
			}
			cur = g.P
		}
		if s.Closed && pts[len(pts)-1] != s.Start {
			pts = append(pts, s.Start)
		}
		out = append(out, Polyline{Pts: pts, Closed: s.Closed})
	}
	return out
}

// flattenCubic appends points of the cubic (excluding p0). The chord error of
// n uniform steps is bounded by 0.75*M/n² where M is the largest second
// difference of the control polygon.
func flattenCubic(dst []Pt, p0, c1, c2, p3 Pt, tol float64) []Pt {
	d1 := p0.Sub(c1.Mul(2)).Add(c2).Len()
	d2 := c1.Sub(c2.Mul(2)).Add(p3).Len()
	n := int(math.Ceil(math.Sqrt(0.75 * max(d1, d2) / tol)))
	n = min(max(n, 1), 1000)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		dst = append(dst, Pt{
			u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p3.X,
			u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p3.Y,
		})
	}
	return dst
}

// --- shape constructors -----------------------------------------------------

const kappa = 0.5522847498307936 // 4/3*(sqrt(2)-1): cubic approximation of a quarter circle

// EllipsePath starts at (cx+rx, cy) and runs in SVG's positive direction.
func EllipsePath(cx, cy, rx, ry float64) Path {
	kx, ky := rx*kappa, ry*kappa
	s := Sub{Start: Pt{cx + rx, cy}, Closed: true}
	s.Segs = []Seg{
		{Cubic, Pt{cx + rx, cy + ky}, Pt{cx + kx, cy + ry}, Pt{cx, cy + ry}},
		{Cubic, Pt{cx - kx, cy + ry}, Pt{cx - rx, cy + ky}, Pt{cx - rx, cy}},
		{Cubic, Pt{cx - rx, cy - ky}, Pt{cx - kx, cy - ry}, Pt{cx, cy - ry}},
		{Cubic, Pt{cx + kx, cy - ry}, Pt{cx + rx, cy - ky}, Pt{cx + rx, cy}},
	}
	return Path{s}
}

// RectPath builds a rectangle, with rounded corners when rx/ry > 0.
func RectPath(x, y, w, h, rx, ry float64) Path {
	rx, ry = min(rx, w/2), min(ry, h/2)
	if rx <= 0 || ry <= 0 {
		return PolyPath([]Pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, true)
	}
	kx, ky := rx*kappa, ry*kappa
	s := Sub{Start: Pt{x + rx, y}, Closed: true}
	s.Segs = []Seg{
		{Kind: Line, P: Pt{x + w - rx, y}},
		{Cubic, Pt{x + w - rx + kx, y}, Pt{x + w, y + ry - ky}, Pt{x + w, y + ry}},
		{Kind: Line, P: Pt{x + w, y + h - ry}},
		{Cubic, Pt{x + w, y + h - ry + ky}, Pt{x + w - rx + kx, y + h}, Pt{x + w - rx, y + h}},
		{Kind: Line, P: Pt{x + rx, y + h}},
		{Cubic, Pt{x + rx - kx, y + h}, Pt{x, y + h - ry + ky}, Pt{x, y + h - ry}},
		{Kind: Line, P: Pt{x, y + ry}},
		{Cubic, Pt{x, y + ry - ky}, Pt{x + rx - kx, y}, Pt{x + rx, y}},
	}
	return Path{s}
}

// PolyPath builds a single subpath through pts.
func PolyPath(pts []Pt, closed bool) Path {
	if len(pts) == 0 {
		return nil
	}
	s := Sub{Start: pts[0], Closed: closed}
	for _, p := range pts[1:] {
		s.Segs = append(s.Segs, Seg{Kind: Line, P: p})
	}
	return Path{s}
}

// --- SVG path data parser ---------------------------------------------------

// ParsePath parses SVG path data ("d" attribute) with every command of SVG 1.1.
func ParsePath(d string) (Path, error) {
	lx := &lexer{s: d}
	var (
		path       Path
		cur        *Sub
		pos, start Pt
		lastCtrl   Pt // reflected control point for S/T
		lastCmd    byte
		cmd        byte
	)
	ensure := func() {
		if cur == nil {
			path = append(path, Sub{Start: pos})
			cur = &path[len(path)-1]
		}
	}
	add := func(g Seg) {
		ensure()
		cur.Segs = append(cur.Segs, g)
		pos = g.P
	}
	for {
		lx.skipSep()
		if lx.done() {
			break
		}
		if c := lx.peek(); isCmd(c) {
			cmd = c
			lx.i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("path data must start with a command, got %q at offset %d", c, lx.i)
		}
		rel := cmd >= 'a'
		base := Pt{}
		if rel {
			base = pos
		}
		switch cmd | 0x20 {
		case 'm':
			p, err := lx.point()
			if err != nil {
				return nil, err
			}
			pos = base.Add(p)
			start = pos
			path = append(path, Sub{Start: pos})
			cur = &path[len(path)-1]
			// Further coordinate pairs after M are implicit L commands.
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
			lastCtrl = pos
			lastCmd = 'm'
			continue
		case 'z':
			if cur != nil {
				cur.Closed = true
				cur = nil
			}
			pos = start
			lastCtrl = pos
			lastCmd = 'z'
			cmd = 0 // a number after Z is an error
			continue
		case 'l':
			p, err := lx.point()
			if err != nil {
				return nil, err
			}
			add(Seg{Kind: Line, P: base.Add(p)})
		case 'h':
			x, err := lx.number()
			if err != nil {
				return nil, err
			}
			if rel {
				x += pos.X
			}
			add(Seg{Kind: Line, P: Pt{x, pos.Y}})
		case 'v':
			y, err := lx.number()
			if err != nil {
				return nil, err
			}
			if rel {
				y += pos.Y
			}
			add(Seg{Kind: Line, P: Pt{pos.X, y}})
		case 'c':
			ps, err := lx.points(3)
			if err != nil {
				return nil, err
			}
			c2 := base.Add(ps[1])
			add(Seg{Cubic, base.Add(ps[0]), c2, base.Add(ps[2])})
			lastCtrl = c2
			lastCmd = 'c'
			continue
		case 's':
			ps, err := lx.points(2)
			if err != nil {
				return nil, err
			}
			c1 := pos
			if lastCmd == 'c' {
				c1 = pos.Mul(2).Sub(lastCtrl)
			}
			c2 := base.Add(ps[0])
			add(Seg{Cubic, c1, c2, base.Add(ps[1])})
			lastCtrl = c2
			lastCmd = 'c'
			continue
		case 'q':
			ps, err := lx.points(2)
			if err != nil {
				return nil, err
			}
			q := base.Add(ps[0])
			add(quadSeg(pos, q, base.Add(ps[1])))
			lastCtrl = q
			lastCmd = 'q'
			continue
		case 't':
			p, err := lx.point()
			if err != nil {
				return nil, err
			}
			q := pos
			if lastCmd == 'q' {
				q = pos.Mul(2).Sub(lastCtrl)
			}
			add(quadSeg(pos, q, base.Add(p)))
			lastCtrl = q
			lastCmd = 'q'
			continue
		case 'a':
			rx, err := lx.number()
			if err != nil {
				return nil, err
			}
			ry, err := lx.number()
			if err != nil {
				return nil, err
			}
			rot, err := lx.number()
			if err != nil {
				return nil, err
			}
			large, err := lx.flag()
			if err != nil {
				return nil, err
			}
			sweep, err := lx.flag()
			if err != nil {
				return nil, err
			}
			p, err := lx.point()
			if err != nil {
				return nil, err
			}
			end := base.Add(p)
			ensure()
			cur.Segs = append(cur.Segs, arcSegs(pos, end, rx, ry, rot, large, sweep)...)
			pos = end
		default:
			return nil, fmt.Errorf("unknown path command %q", cmd)
		}
		lastCtrl = pos
		lastCmd = cmd | 0x20
	}
	return path, nil
}

func quadSeg(p0, q, p2 Pt) Seg {
	return Seg{Cubic, p0.Add(q.Sub(p0).Mul(2.0 / 3)), p2.Add(q.Sub(p2).Mul(2.0 / 3)), p2}
}

// arcSegs converts an SVG endpoint arc to cubics of at most 90° each,
// following the SVG 1.1 implementation notes (F.6.5, F.6.6).
func arcSegs(p0, p1 Pt, rx, ry, rotDeg float64, large, sweep bool) []Seg {
	if p0 == p1 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []Seg{{Kind: Line, P: p1}}
	}
	sinR, cosR := math.Sincos(rotDeg * math.Pi / 180)
	dx, dy := (p0.X-p1.X)/2, (p0.Y-p1.Y)/2
	x1 := cosR*dx + sinR*dy
	y1 := -sinR*dx + cosR*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	co := math.Sqrt(max(num/den, 0))
	if large == sweep {
		co = -co
	}
	cx1, cy1 := co*rx*y1/ry, -co*ry*x1/rx
	cx := cosR*cx1 - sinR*cy1 + (p0.X+p1.X)/2
	cy := sinR*cx1 + cosR*cy1 + (p0.Y+p1.Y)/2

	angle := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	th1 := angle(1, 0, (x1-cx1)/rx, (y1-cy1)/ry)
	dth := angle((x1-cx1)/rx, (y1-cy1)/ry, (-x1-cx1)/rx, (-y1-cy1)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}

	n := int(math.Ceil(math.Abs(dth) / (math.Pi / 2)))
	step := dth / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(th float64) (Pt, Pt) { // point and derivative direction
		s, c := math.Sincos(th)
		x, y := rx*c, ry*s
		tx, ty := -rx*s, ry*c
		return Pt{cosR*x - sinR*y + cx, sinR*x + cosR*y + cy}, Pt{cosR*tx - sinR*ty, sinR*tx + cosR*ty}
	}
	segs := make([]Seg, 0, n)
	a, da := pt(th1)
	for i := 1; i <= n; i++ {
		b, db := pt(th1 + step*float64(i))
		if i == n {
			b = p1
		}
		segs = append(segs, Seg{Cubic, a.Add(da.Mul(k)), b.Sub(db.Mul(k)), b})
		a, da = b, db
	}
	return segs
}

// --- lexer ------------------------------------------------------------------

type lexer struct {
	s string
	i int
}

func isCmd(c byte) bool {
	switch c | 0x20 {
	case 'm', 'z', 'l', 'h', 'v', 'c', 's', 'q', 't', 'a':
		return true
	}
	return false
}

func (l *lexer) done() bool { return l.i >= len(l.s) }
func (l *lexer) peek() byte { return l.s[l.i] }

func (l *lexer) skipSep() {
	for l.i < len(l.s) {
		switch l.s[l.i] {
		case ' ', '\t', '\n', '\r', ',':
			l.i++
		default:
			return
		}
	}
}

// number scans an SVG number: "-1.5e3", ".5", and "1.5.5" (= 1.5 then .5).
func (l *lexer) number() (float64, error) {
	l.skipSep()
	st := l.i
	if l.i < len(l.s) && (l.s[l.i] == '+' || l.s[l.i] == '-') {
		l.i++
	}
	digits, dot := false, false
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' && !dot:
			dot = true
		default:
			goto exp
		}
		l.i++
	}
exp:
	if digits && l.i < len(l.s) && (l.s[l.i] == 'e' || l.s[l.i] == 'E') {
		j := l.i + 1
		if j < len(l.s) && (l.s[j] == '+' || l.s[j] == '-') {
			j++
		}
		if j < len(l.s) && l.s[j] >= '0' && l.s[j] <= '9' {
			for j < len(l.s) && l.s[j] >= '0' && l.s[j] <= '9' {
				j++
			}
			l.i = j
		}
	}
	if !digits {
		l.i = st
		return 0, fmt.Errorf("expected number at offset %d in path data", st)
	}
	return strconv.ParseFloat(l.s[st:l.i], 64)
}

// flag scans an arc flag, which may be glued to the next number ("a1 1 0 01 5 5").
func (l *lexer) flag() (bool, error) {
	l.skipSep()
	if l.i < len(l.s) && (l.s[l.i] == '0' || l.s[l.i] == '1') {
		l.i++
		return l.s[l.i-1] == '1', nil
	}
	return false, fmt.Errorf("expected arc flag 0 or 1 at offset %d in path data", l.i)
}

func (l *lexer) point() (Pt, error) {
	x, err := l.number()
	if err != nil {
		return Pt{}, err
	}
	y, err := l.number()
	if err != nil {
		return Pt{}, err
	}
	return Pt{x, y}, nil
}

func (l *lexer) points(n int) ([]Pt, error) {
	ps := make([]Pt, n)
	for i := range n {
		p, err := l.point()
		if err != nil {
			return nil, err
		}
		ps[i] = p
	}
	return ps, nil
}
