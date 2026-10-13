package render

import (
	"math"
	"strings"

	"github.com/tazhate/serious-animator/internal/ease"
	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/project"
)

// Camera framing limits, in viewBox units.
const (
	minViewShort  = 562.5 // closest zoom, short side: captions stay large, not huge
	viewPadFrac   = 0.12
	viewPadMin    = 60.0
	moveMin       = 0.8
	moveMax       = 1.5
	zoomOutDur    = 1.4
	zoomOutTail   = 1.6 // extra hold at the end of a camera scene
	keepZoomRatio = 1.3 // no move when the target fits and is not much smaller
)

type camMove struct {
	t0, t1   float64
	from, to geom.Rect
}

// camera is the planned view of a scene over time, in viewBox units.
type camera struct {
	start geom.Rect
	moves []camMove
}

func (c *camera) at(t float64) geom.Rect {
	v := c.start
	for _, mv := range c.moves {
		if t < mv.t0 {
			break
		}
		if t >= mv.t1 {
			v = mv.to
			continue
		}
		return lerpView(mv.from, mv.to, ease.InOutCubic((t-mv.t0)/(mv.t1-mv.t0)))
	}
	return v
}

// lerpView moves the center linearly and zooms geometrically, so a zoom by
// 2x feels as fast in its first half as in its second.
func lerpView(a, b geom.Rect, k float64) geom.Rect {
	c := center(a).Lerp(center(b), k)
	w := a.W() * math.Pow(b.W()/a.W(), k)
	h := a.H() * math.Pow(b.H()/a.H(), k)
	return rectAt(c, w, h)
}

func center(r geom.Rect) geom.Pt { return r.Min.Lerp(r.Max, 0.5) }

func rectAt(c geom.Pt, w, h float64) geom.Rect {
	return geom.Rect{Min: geom.Pt{X: c.X - w/2, Y: c.Y - h/2}, Max: geom.Pt{X: c.X + w/2, Y: c.Y + h/2}}
}

// boardView is the whole board at the output aspect ratio.
func (sc *Scene) boardView() geom.Rect {
	return fitAspect(sc.vb, sc.aspect())
}

func (sc *Scene) aspect() float64 { return float64(sc.w) / float64(sc.h) }

// fitAspect grows r to the aspect ratio a around its center.
func fitAspect(r geom.Rect, a float64) geom.Rect {
	w, h := r.W(), r.H()
	if w/h < a {
		w = h * a
	} else {
		h = w / a
	}
	return rectAt(center(r), w, h)
}

// frameBox turns the bounds of what a beat draws into a camera view: padded,
// at the output aspect, not closer than minViewShort, inside the board.
func (sc *Scene) frameBox(r geom.Rect) geom.Rect {
	board := sc.boardView()
	pad := max(viewPadFrac*max(r.W(), r.H()), viewPadMin)
	v := fitAspect(r.Inset(pad), sc.aspect())
	if s := min(v.W(), v.H()); s < minViewShort {
		k := minViewShort / s
		v = rectAt(center(v), v.W()*k, v.H()*k)
	}
	if v.W() >= board.W() || v.H() >= board.H() {
		return board
	}
	// Slide the view back inside the board.
	dx := max(board.Min.X-v.Min.X, 0) - max(v.Max.X-board.Max.X, 0)
	dy := max(board.Min.Y-v.Min.Y, 0) - max(v.Max.Y-board.Max.Y, 0)
	return geom.Rect{Min: v.Min.Add(geom.Pt{X: dx, Y: dy}), Max: v.Max.Add(geom.Pt{X: dx, Y: dy})}
}

// beatViews returns the view each beat wants and what it must show;
// ok[b] is false for "hold".
func (sc *Scene) beatViews() (views, content []geom.Rect, ok []bool) {
	views = make([]geom.Rect, len(sc.beatCams))
	content = make([]geom.Rect, len(sc.beatCams))
	ok = make([]bool, len(sc.beatCams))
	for b, cam := range sc.beatCams {
		r := geom.EmptyRect()
		switch {
		case cam == project.BeatCameraBoard:
			r = sc.boardView()
		case cam == project.BeatCameraHold:
		case strings.HasPrefix(cam, "#"):
			if box, found := sc.boxes[cam[1:]]; found {
				r = box
			}
		default:
			for _, it := range sc.items {
				if it.src.Beat == b+1 {
					r = r.Union(it.bounds)
				}
			}
		}
		if !r.Empty() {
			views[b], content[b], ok[b] = sc.frameBox(r), r, true
		}
	}
	// A beat with nothing to frame keeps the previous view.
	last := sc.boardView()
	for b := range views {
		if ok[b] {
			last = views[b]
			continue
		}
		if b == 0 {
			ok[0] = true
		}
		views[b] = last
	}
	return views, content, ok
}

func contains(outer, inner geom.Rect) bool {
	const eps = 1e-6
	return inner.Min.X >= outer.Min.X-eps && inner.Min.Y >= outer.Min.Y-eps &&
		inner.Max.X <= outer.Max.X+eps && inner.Max.Y <= outer.Max.Y+eps
}

// planCamera moves the view to each beat just before it is drawn, and pulls
// back to the whole board at the end of the scene as a recap. A beat whose
// drawing would start before the camera gets there is delayed to match.
func (sc *Scene) planCamera() camera {
	if !sc.cam {
		return camera{start: sc.vb}
	}
	views, content, set := sc.beatViews()
	c := camera{start: views[0]}
	cur := views[0]
	lastEnd := 0.0
	doneBy := func(b int) float64 { // when beats before b finish drawing
		t := 0.0
		for i, it := range sc.items {
			if it.src.Beat >= 1 && it.src.Beat <= b {
				t = max(t, sc.spans[i].End())
			}
		}
		return t
	}
	for b := 1; b < len(views); b++ {
		target := views[b]
		if !set[b] || (contains(cur, content[b].Inset(viewPadMin/2)) && cur.W() <= keepZoomRatio*target.W()) {
			continue
		}
		dur := moveDur(cur, target)
		t0 := max(sc.beats[b].DrawStart-dur, doneBy(b), lastEnd)
		c.moves = append(c.moves, camMove{t0: t0, t1: t0 + dur, from: cur, to: target})
		cur, lastEnd = target, t0+dur
		sc.delayBeat(b+1, lastEnd-arriveEarly)
	}
	board := sc.boardView()
	if cur != board {
		limit := sc.dur - sc.transOut - 0.2
		t0 := max(doneBy(len(views))+0.4, lastEnd)
		t1 := min(t0+zoomOutDur, limit)
		if t1-t0 >= 0.5 {
			c.moves = append(c.moves, camMove{t0: t0, t1: t1, from: cur, to: board})
		}
	}
	return c
}

// arriveEarly lets drawing begin just before the camera settles.
const arriveEarly = 0.25

// delayBeat makes the items of beat (1-based) start no earlier than from,
// keeping their order and squeezing them if the beat window is too short.
func (sc *Scene) delayBeat(beat int, from float64) {
	var idx []int
	first, last := math.Inf(1), math.Inf(-1)
	for i, it := range sc.items {
		if it.src.Beat == beat {
			idx = append(idx, i)
			first, last = min(first, sc.spans[i].Start), max(last, sc.spans[i].End())
		}
	}
	if len(idx) == 0 || first >= from {
		return
	}
	end := max(sc.beats[beat-1].DrawEnd, from+0.3)
	k := 1.0
	if last-first > 0 && from+(last-first) > end {
		k = (end - from) / (last - first)
	}
	for _, i := range idx {
		sp := &sc.spans[i]
		sp.Start = from + (sp.Start-first)*k
		sp.Dur *= k
	}
}

func moveDur(a, b geom.Rect) float64 {
	dist := center(a).Dist(center(b)) / a.W()
	zoom := math.Abs(math.Log(b.W() / a.W()))
	return min(max(moveMin+0.5*dist+0.4*zoom, moveMin), moveMax)
}
