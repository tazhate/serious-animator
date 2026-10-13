package geom

import (
	"cmp"
	"math"
	"slices"
)

// Hachure covers the inside of polys (nonzero rule) with parallel lines gap
// apart, running at deg degrees from the x axis. Lines come back in drawing
// order and alternate direction, the way a hand scribbles a fill back and
// forth. Every line is a two-point polyline inside the shape.
func Hachure(polys [][]Pt, deg, gap float64) [][]Pt {
	if gap <= 0 {
		return nil
	}
	// Work in a frame where the hatch lines are horizontal.
	toFlat, back := Rotate(-deg), Rotate(deg)
	type edge struct{ a, b Pt }
	var edges []edge
	box := EmptyRect()
	for _, p := range polys {
		if len(p) < 3 {
			continue
		}
		for i := range p {
			a, b := toFlat.Apply(p[i]), toFlat.Apply(p[(i+1)%len(p)])
			edges = append(edges, edge{a, b})
			box = box.Extend(a)
		}
	}
	if box.Empty() {
		return nil
	}
	type crossing struct {
		x    float64
		wind int
	}
	var (
		out  [][]Pt
		xs   []crossing
		flip bool
	)
	// Center the lines in the shape so thin shapes still get a line.
	n := math.Floor(box.H() / gap)
	y0 := box.Min.Y + (box.H()-n*gap)/2
	for y := y0; y < box.Max.Y; y += gap {
		xs = xs[:0]
		for _, e := range edges {
			if (e.a.Y <= y) == (e.b.Y <= y) {
				continue
			}
			x := e.a.X + (y-e.a.Y)/(e.b.Y-e.a.Y)*(e.b.X-e.a.X)
			w := 1
			if e.b.Y < e.a.Y {
				w = -1
			}
			xs = append(xs, crossing{x, w})
		}
		slices.SortFunc(xs, func(p, q crossing) int { return cmp.Compare(p.x, q.x) })
		var row [][]Pt
		wind, start := 0, 0.0
		for _, c := range xs {
			prev := wind
			wind += c.wind
			switch {
			case prev == 0 && wind != 0:
				start = c.x
			case prev != 0 && wind == 0 && c.x-start > gap*0.25:
				row = append(row, []Pt{back.Apply(Pt{start, y}), back.Apply(Pt{c.x, y})})
			}
		}
		if flip {
			slices.Reverse(row)
			for _, l := range row {
				l[0], l[1] = l[1], l[0]
			}
		}
		if len(row) > 0 {
			flip = !flip
		}
		out = append(out, row...)
	}
	return out
}
