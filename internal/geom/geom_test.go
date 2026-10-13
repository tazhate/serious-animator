package geom

import (
	"math"
	"testing"
)

func pathLength(p Path, tol float64) float64 {
	total := 0.0
	for _, pl := range p.Flatten(tol) {
		c := Cumulative(pl.Pts)
		total += c[len(c)-1]
	}
	return total
}

func near(t *testing.T, name string, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s = %.4f, want %.4f ±%.4f", name, got, want, eps)
	}
}

func TestEllipseLength(t *testing.T) {
	near(t, "circle r=100", pathLength(EllipsePath(0, 0, 100, 100), 0.05), 2*math.Pi*100, 0.5)
}

func TestParsePathCommands(t *testing.T) {
	cases := []struct {
		d       string
		length  float64
		subs    int
		closed  bool
		endX    float64
		endY    float64
		epsilon float64
	}{
		{d: "M0 0 L10 0 L10 10", length: 20, subs: 1, endX: 10, endY: 10},
		{d: "M0,0 h10 v10 h-10 z", length: 40, subs: 1, closed: true, endX: 0, endY: 10},
		{d: "m0 0 10 0 0 10", length: 20, subs: 1, endX: 10, endY: 10},
		{d: "M-1-2L-1.5.5", length: math.Hypot(0.5, 2.5), subs: 1, endX: -1.5, endY: 0.5},
		{d: "M0 0 1e1 0", length: 10, subs: 1, endX: 10, endY: 0},
		// Half circle of radius 50 through an arc, with glued flags.
		{d: "M0 0 a50 50 0 01100 0", length: math.Pi * 50, subs: 1, endX: 100, endY: 0, epsilon: 0.3},
		// Quadratic + smooth quadratic, then cubic + smooth cubic: just check endpoints.
		{d: "M0 0 Q5 10 10 0 T20 0", subs: 1, endX: 20, endY: 0, length: -1},
		{d: "M0 0 C0 10 10 10 10 0 S20 -10 20 0", subs: 1, endX: 20, endY: 0, length: -1},
		{d: "M0 0 L1 0 M5 5 L6 5", length: 2, subs: 2, endX: 6, endY: 5},
	}
	for _, c := range cases {
		p, err := ParsePath(c.d)
		if err != nil {
			t.Fatalf("%q: %v", c.d, err)
		}
		if len(p) != c.subs {
			t.Fatalf("%q: %d subpaths, want %d", c.d, len(p), c.subs)
		}
		last := p[len(p)-1]
		if last.Closed != c.closed {
			t.Errorf("%q: closed = %v", c.d, last.Closed)
		}
		end := last.Start
		if n := len(last.Segs); n > 0 {
			end = last.Segs[n-1].P
		}
		near(t, c.d+" end.x", end.X, c.endX, 1e-9)
		near(t, c.d+" end.y", end.Y, c.endY, 1e-9)
		if c.length >= 0 {
			near(t, c.d+" length", pathLength(p, 0.01), c.length, max(c.epsilon, 1e-6))
		}
	}
}

func TestParsePathErrors(t *testing.T) {
	for _, d := range []string{"10 10", "M0 0 L", "M0 0 X5 5", "M0 0 a1 1 0 2 0 5 5"} {
		if _, err := ParsePath(d); err == nil {
			t.Errorf("%q: expected error", d)
		}
	}
}

func TestPrefix(t *testing.T) {
	pts := []Pt{{0, 0}, {10, 0}, {10, 10}}
	cum := Cumulative(pts)
	got := Prefix(pts, cum, 15)
	if len(got) != 3 || got[2] != (Pt{10, 5}) {
		t.Fatalf("Prefix(15) = %v", got)
	}
	if got := Prefix(pts, cum, 0); got != nil {
		t.Fatalf("Prefix(0) = %v", got)
	}
	if got := Prefix(pts, cum, 99); len(got) != 3 {
		t.Fatalf("Prefix(99) = %v", got)
	}
}

func TestTransform(t *testing.T) {
	m := Translate(10, 0).Mul(Rotate(90))
	p := m.Apply(Pt{1, 0})
	near(t, "x", p.X, 10, 1e-9)
	near(t, "y", p.Y, 1, 1e-9)
	near(t, "scale", ScaleXY(2, 8).Scale(), 4, 1e-9)
}

func TestRoughenKeepsEndsAndClosure(t *testing.T) {
	open := Polyline{Pts: []Pt{{0, 0}, {200, 0}}}
	r := Roughen(open, 3, 40, 7)
	if r.Pts[0] != open.Pts[0] || r.Pts[len(r.Pts)-1] != open.Pts[1] {
		t.Fatalf("open ends moved: %v %v", r.Pts[0], r.Pts[len(r.Pts)-1])
	}
	moved := false
	for _, p := range r.Pts {
		if math.Abs(p.Y) > 0.1 {
			moved = true
		}
		if math.Abs(p.Y) > 3+1e-9 {
			t.Fatalf("offset %v exceeds amplitude", p.Y)
		}
	}
	if !moved {
		t.Fatal("roughen did nothing")
	}
	closed := EllipsePath(0, 0, 50, 50).Flatten(0.1)[0]
	rc := Roughen(closed, 3, 40, 7)
	if rc.Pts[0].Dist(rc.Pts[len(rc.Pts)-1]) > 1e-9 {
		t.Fatal("closed polyline opened up")
	}
}

func TestHachureStaysInside(t *testing.T) {
	// An L-shaped polygon: lines must not cross the notch.
	poly := []Pt{{0, 0}, {100, 0}, {100, 40}, {40, 40}, {40, 100}, {0, 100}}
	inside := func(p Pt) bool {
		return p.X >= -1e-6 && p.Y >= -1e-6 && p.X <= 100+1e-6 && p.Y <= 100+1e-6 && (p.X <= 40+1e-6 || p.Y <= 40+1e-6)
	}
	lines := Hachure([][]Pt{poly}, -41, 8)
	if len(lines) < 10 {
		t.Fatalf("only %d lines", len(lines))
	}
	for _, l := range lines {
		for k := range 11 {
			if p := l[0].Lerp(l[1], float64(k)/10); !inside(p) {
				t.Fatalf("line %v leaves the shape at %v", l, p)
			}
		}
	}
	// Consecutive rows run in opposite directions.
	d0 := lines[0][1].Sub(lines[0][0])
	d1 := lines[1][1].Sub(lines[1][0])
	if d0.X*d1.X+d0.Y*d1.Y >= 0 {
		t.Fatal("rows do not alternate direction")
	}
}
