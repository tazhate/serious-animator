// Package geom holds the vector math shared by the SVG parser and the
// rasterizer: points, affine transforms, path segments, flattening and
// arc-length queries on polylines.
package geom

import "math"

// Pt is a 2-D point or vector.
type Pt struct{ X, Y float64 }

func (p Pt) Add(q Pt) Pt             { return Pt{p.X + q.X, p.Y + q.Y} }
func (p Pt) Sub(q Pt) Pt             { return Pt{p.X - q.X, p.Y - q.Y} }
func (p Pt) Mul(k float64) Pt        { return Pt{p.X * k, p.Y * k} }
func (p Pt) Len() float64            { return math.Hypot(p.X, p.Y) }
func (p Pt) Dist(q Pt) float64       { return p.Sub(q).Len() }
func (p Pt) Lerp(q Pt, t float64) Pt { return Pt{p.X + (q.X-p.X)*t, p.Y + (q.Y-p.Y)*t} }

// Mat is an affine transform in SVG order (a b c d e f):
//
//	x' = a*x + c*y + e
//	y' = b*x + d*y + f
type Mat [6]float64

// Identity is the identity transform.
var Identity = Mat{1, 0, 0, 1, 0, 0}

// Apply maps a point through the transform.
func (m Mat) Apply(p Pt) Pt {
	return Pt{m[0]*p.X + m[2]*p.Y + m[4], m[1]*p.X + m[3]*p.Y + m[5]}
}

// Mul returns m * n: n is applied first, then m.
func (m Mat) Mul(n Mat) Mat {
	return Mat{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// Scale returns the mean linear scale factor, used for stroke widths.
func (m Mat) Scale() float64 {
	return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2]))
}

func Translate(x, y float64) Mat { return Mat{1, 0, 0, 1, x, y} }
func ScaleXY(x, y float64) Mat   { return Mat{x, 0, 0, y, 0, 0} }

// Rotate returns a rotation by deg degrees around the origin.
func Rotate(deg float64) Mat {
	s, c := math.Sincos(deg * math.Pi / 180)
	return Mat{c, s, -s, c, 0, 0}
}

// Rect is an axis-aligned box. An empty Rect has Min > Max.
type Rect struct{ Min, Max Pt }

// EmptyRect returns a rect that any Extend call replaces.
func EmptyRect() Rect {
	return Rect{Pt{math.Inf(1), math.Inf(1)}, Pt{math.Inf(-1), math.Inf(-1)}}
}

func (r Rect) Empty() bool { return r.Min.X > r.Max.X || r.Min.Y > r.Max.Y }
func (r Rect) W() float64  { return r.Max.X - r.Min.X }
func (r Rect) H() float64  { return r.Max.Y - r.Min.Y }

// Extend grows r to include p.
func (r Rect) Extend(p Pt) Rect {
	return Rect{
		Pt{min(r.Min.X, p.X), min(r.Min.Y, p.Y)},
		Pt{max(r.Max.X, p.X), max(r.Max.Y, p.Y)},
	}
}

// Union returns the smallest rect containing r and s.
func (r Rect) Union(s Rect) Rect {
	if s.Empty() {
		return r
	}
	return r.Extend(s.Min).Extend(s.Max)
}

// Intersect returns the overlap of r and s, possibly empty.
func (r Rect) Intersect(s Rect) Rect {
	return Rect{
		Pt{max(r.Min.X, s.Min.X), max(r.Min.Y, s.Min.Y)},
		Pt{min(r.Max.X, s.Max.X), min(r.Max.Y, s.Max.Y)},
	}
}

// Area is zero for empty rects.
func (r Rect) Area() float64 {
	if r.Empty() {
		return 0
	}
	return r.W() * r.H()
}

// Inset grows (d > 0) or shrinks the rect on every side.
func (r Rect) Inset(d float64) Rect {
	return Rect{Pt{r.Min.X - d, r.Min.Y - d}, Pt{r.Max.X + d, r.Max.Y + d}}
}
