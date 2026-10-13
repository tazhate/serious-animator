// Package render turns parsed scenes into frames and encodes the video.
package render

import (
	"cmp"
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/tazhate/serious-animator/internal/ease"
	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/project"
	"github.com/tazhate/serious-animator/internal/raster"
	"github.com/tazhate/serious-animator/internal/svgscene"
	"github.com/tazhate/serious-animator/internal/timeline"
)

// Quality is an output preset.
type Quality struct {
	Name string
	W, H int
	FPS  int
	CRF  int
}

// Qualities are the presets accepted by --quality.
var Qualities = map[string]Quality{
	"low":    {Name: "low", W: 854, H: 480, FPS: 20, CRF: 26},
	"medium": {Name: "medium", W: 1280, H: 720, FPS: 24, CRF: 21},
	"high":   {Name: "high", W: 1920, H: 1080, FPS: 30, CRF: 19},
}

// DeviceMat maps the viewBox onto a w×h canvas, keeping the aspect ratio and
// centering (preserveAspectRatio="xMidYMid meet").
func DeviceMat(vb geom.Rect, w, h int) geom.Mat {
	s := min(float64(w)/vb.W(), float64(h)/vb.H())
	ox := (float64(w) - vb.W()*s) / 2
	oy := (float64(h) - vb.H()*s) / 2
	return geom.Translate(ox, oy).Mul(geom.ScaleXY(s, s)).Mul(geom.Translate(-vb.Min.X, -vb.Min.Y))
}

// Look and motion constants.
var accent = color.NRGBA{0xe0, 0x31, 0x31, 0xff}

const (
	dimLevel     = 0.32 // opacity of earlier beats in a dimmed scene
	dimTime      = 0.45
	hlHold       = 0.6 // a highlight stays this long after its beat
	hlFade       = 0.5
	markerTravel = 1.2  // longer pauses send the marker off the board
	markerExit   = 0.45 // seconds to leave or enter the board
	transDefault = project.TransitionSwipe
)

// SceneConfig is what PrepareScene needs besides the drawing.
type SceneConfig struct {
	ID         string
	W, H       int
	Roughness  float64
	Camera     bool
	Dim        bool
	Transition string
	BeatCams   []string // per beat: "", "board", "hold" or "#id"
}

// Scene is a drawing prepared for one output size. Geometry stays in viewBox
// units; every frame is rasterized through the camera of that moment.
type Scene struct {
	id    string
	vb    geom.Rect
	w, h  int
	s720  float64 // output height / 720, for the marker size
	unit  float64 // 720p pixels per viewBox unit in the reference view
	items []*item
	boxes map[string]geom.Rect

	cam      bool
	dim      bool
	trans    string
	beatCams []string

	// Timing, set by Plan.
	beats     []timeline.Beat
	spans     []timeline.Span
	dur       float64
	transOut  float64
	camera    camera
	beatStart []float64 // when each beat's first element starts
	marks     []int     // items with a marker path, by start time
}

// PrepareScene flattens and roughens the drawing once.
func PrepareScene(d *svgscene.Scene, cfg SceneConfig) *Scene {
	sc := &Scene{
		id: cfg.ID, vb: d.ViewBox, w: cfg.W, h: cfg.H, s720: float64(cfg.H) / 720,
		boxes: d.Boxes, cam: cfg.Camera, dim: cfg.Dim, trans: cfg.Transition, beatCams: cfg.BeatCams,
	}
	if sc.trans == "" {
		sc.trans = transDefault
	}
	ref := d.ViewBox
	if sc.cam {
		ref = geom.Rect{Max: geom.Pt{X: project.FrameW, Y: project.FrameH}}
	}
	sc.unit = min(1280/ref.W(), 720/ref.H())
	seed := geom.HashString(cfg.ID)
	motions := make([]*motion, len(d.Motions))
	for i := range d.Motions {
		motions[i] = newMotion(&d.Motions[i])
	}
	for i := range d.Items {
		it := &d.Items[i]
		pi := prepare(it, sc.unit, cfg.Roughness, seed^geom.Hash64(uint64(i)))
		if it.Motion > 0 {
			pi.motion = motions[it.Motion-1]
		}
		if it.Anim == "pop" {
			pi.popSFX = true
		}
		if n := len(sc.items); n > 0 && it.Anim == "" {
			if tip, ok := arrowHead(sc.items[n-1], pi); ok {
				pi.anim, pi.pivot, pi.natural, pi.hasTip = "pop", tip, popTime, false
			}
		}
		sc.items = append(sc.items, pi)
	}
	for i, h := range d.Highlights {
		it := highlightItem(h, geom.Hash64(seed+uint64(i)))
		pi := prepare(it, sc.unit, cfg.Roughness, seed^geom.Hash64(uint64(1000+i)))
		pi.hl, pi.taper = true, true
		sc.items = append(sc.items, pi)
	}
	if len(sc.beatCams) == 0 {
		sc.beatCams = make([]string, maxBeat(sc.items))
	}
	return sc
}

func maxBeat(items []*item) int {
	n := 1
	for _, it := range items {
		n = max(n, it.src.Beat)
	}
	return n
}

// arrowHead recognizes the "V" drawn right after an arrow's shaft: a short
// open two-segment stroke whose corner sits on the shaft's end. Arrowheads
// pop in around that corner instead of being drawn.
func arrowHead(prev, cur *item) (geom.Pt, bool) {
	src := cur.src
	if src.Beat != prev.src.Beat || cur.text || src.HasFill || !src.HasStroke || len(src.Path) != 1 {
		return geom.Pt{}, false
	}
	sub := src.Path[0]
	if sub.Closed || len(sub.Segs) != 2 || sub.Segs[0].Kind != geom.Line || sub.Segs[1].Kind != geom.Line || cur.strokeLen > 120 {
		return geom.Pt{}, false
	}
	if prev.text || len(prev.lines) == 0 || prev.fillPolys != nil {
		return geom.Pt{}, false
	}
	tip := sub.Segs[0].P
	last := prev.src.Path[len(prev.src.Path)-1]
	end := last.Start
	if n := len(last.Segs); n > 0 {
		end = last.Segs[n-1].P
	}
	return tip, end.Dist(tip) <= 30
}

// highlightItem builds the hand-drawn circle or underline for a highlight.
func highlightItem(h svgscene.Highlight, seed uint64) *svgscene.Item {
	r := h.Target
	var pts []geom.Pt
	switch h.Kind {
	case "underline":
		y := r.Max.Y + 14
		pts = []geom.Pt{{X: r.Min.X - 12, Y: y + 2}, {X: (r.Min.X + r.Max.X) / 2, Y: y + 6}, {X: r.Max.X + 12, Y: y - 3}}
		pts = geom.Resample(pts, 20)
	default:
		c := center(r)
		pad := 0.1*min(r.W(), r.H()) + 18
		rx, ry := r.W()/2+pad, r.H()/2+pad
		phase := float64(seed%628) / 100
		const turns = 1.12
		n := 96
		start := -2.6 // upper left, where a right hand starts a circle
		for i := range n + 1 {
			f := float64(i) / float64(n)
			a := start + f*turns*2*math.Pi
			k := 1 + 0.035*math.Sin(3*a+phase) + 0.06*f // spirals out so the ends do not meet
			pts = append(pts, geom.Pt{X: c.X + rx*k*math.Cos(a), Y: c.Y + ry*k*math.Sin(a)})
		}
	}
	return &svgscene.Item{
		ID: "highlight", Tag: "highlight", Line: h.Line, Beat: h.Beat,
		Path:      geom.PolyPath(pts, false),
		HasStroke: true, Stroke: accent, StrokeWidth: 6,
		NoDim: true,
	}
}

// Requests returns the scheduling input for every item, in document order.
func (sc *Scene) Requests() []timeline.Req {
	reqs := make([]timeline.Req, len(sc.items))
	for i, it := range sc.items {
		reqs[i] = timeline.Req{Beat: it.src.Beat, Natural: it.natural}
	}
	return reqs
}

// Plan schedules the items into the scene's beats and derives everything
// that depends on time: motion starts, highlight fades, the camera path and
// the marker's route.
func (sc *Scene) Plan(ts timeline.Scene) {
	sc.beats, sc.dur, sc.transOut = ts.Beats, ts.Dur, ts.TransOut
	sc.spans = timeline.Schedule(ts.Beats, sc.Requests())

	sc.camera = sc.planCamera()

	sc.beatStart = make([]float64, len(sc.beats))
	for b := range sc.beatStart {
		sc.beatStart[b] = math.Inf(1)
	}
	for i, it := range sc.items {
		if b := it.src.Beat; b >= 1 && b <= len(sc.beats) {
			sc.beatStart[b-1] = min(sc.beatStart[b-1], sc.spans[i].Start)
		}
	}
	for b := range sc.beatStart {
		if math.IsInf(sc.beatStart[b], 1) {
			sc.beatStart[b] = sc.beats[b].DrawStart
		}
	}

	// A motion starts when its whole group is drawn, or at its beat.
	starts := map[*motion]float64{}
	for i, it := range sc.items {
		if it.motion != nil {
			starts[it.motion] = max(starts[it.motion], sc.spans[i].End())
		}
	}
	for mo, end := range starts {
		mo.start = end
		if b := mo.src.Beat; b >= 1 && b <= len(sc.beats) {
			mo.start = sc.beatStart[b-1]
		}
	}

	sc.marks = sc.marks[:0]
	for i, it := range sc.items {
		if it.hasTip && sc.spans[i].Dur > 0 {
			sc.marks = append(sc.marks, i)
		}
	}
	slices.SortStableFunc(sc.marks, func(a, b int) int { return cmp.Compare(sc.spans[a].Start, sc.spans[b].Start) })
}

// itemState is how an item looks at one moment.
type itemState struct {
	u     float64 // drawing progress; 1 = finished, <=0 = not started
	alpha float64
	m     geom.Mat // viewBox-space transform: motion and pop
	still bool     // the transform does not change around this time
}

func (sc *Scene) state(i int, t float64) itemState {
	it := sc.items[i]
	sp := sc.spans[i]
	st := itemState{alpha: 1, m: geom.Identity, still: true}
	switch {
	case t < sp.Start:
		st.u = 0
		return st
	case sp.Dur <= 0 || t >= sp.End():
		st.u = 1
	default:
		st.u = (t - sp.Start) / sp.Dur
	}
	switch it.anim {
	case "pop":
		if st.u < 1 {
			k := ease.OutBack(st.u)
			p := it.pivot
			st.m = geom.Translate(p.X, p.Y).Mul(geom.ScaleXY(k, k)).Mul(geom.Translate(-p.X, -p.Y))
			st.still = false
		}
	case "fade":
		st.alpha = ease.Smoothstep(st.u)
		st.still = st.u >= 1
	}
	if it.motion != nil {
		st.m = it.motion.mat(t).Mul(st.m)
		st.still = st.still && !it.motion.moving(t)
	}
	if it.hl {
		end := sc.beats[it.src.Beat-1].DrawEnd + hlHold
		st.alpha *= 1 - ease.Smoothstep((t-end)/hlFade)
	}
	st.alpha *= sc.dimFactor(it, t)
	return st
}

// dimFactor fades the elements of earlier beats while a new beat is drawn.
// The last beat brings everything back for the closing look.
func (sc *Scene) dimFactor(it *item, t float64) float64 {
	if !sc.dim || it.src.NoDim || len(sc.beats) < 3 {
		return 1
	}
	cur := -1
	for b, s := range sc.beatStart {
		if s <= t {
			cur = b
		}
	}
	level := func(c int) float64 {
		if c < 0 || c == len(sc.beats)-1 || it.src.Beat-1 >= c {
			return 1
		}
		return dimLevel
	}
	if cur < 0 {
		return 1
	}
	k := ease.Smoothstep((t - sc.beatStart[cur]) / dimTime)
	return level(cur-1) + (level(cur)-level(cur-1))*k
}

// worker renders frames on one goroutine. Its cache keeps the image of the
// longest run of finished, unmoving items from the start of the document.
type worker struct {
	p     raster.Painter
	paper *image.RGBA
	cache struct {
		sc  *Scene
		m   geom.Mat
		k   int
		sig uint64
		img *image.RGBA
	}
}

func newWorker(paper *image.RGBA) *worker { return &worker{paper: paper} }

func sigStep(h uint64, st itemState) uint64 {
	h = geom.Hash64(h ^ uint64(st.alpha*4096))
	for _, v := range st.m {
		h = geom.Hash64(h ^ math.Float64bits(v))
	}
	return h
}

// frame renders scene-local time t into dst. shift slides the drawing
// sideways in pixels (scene transitions).
func (sc *Scene) frame(w *worker, t, shift float64, marker bool, dst *image.RGBA) {
	m := geom.Translate(shift, 0).Mul(DeviceMat(sc.camera.at(t), sc.w, sc.h))
	states := make([]itemState, len(sc.items))
	sigs := make([]uint64, len(sc.items)+1)
	k := 0
	for i := range sc.items {
		states[i] = sc.state(i, t)
		if k == i && states[i].u >= 1 && states[i].still {
			sigs[i+1] = sigStep(sigs[i], states[i])
			k = i + 1
		}
	}

	c := &w.cache
	if c.img == nil || c.img.Rect != w.paper.Rect {
		c.img = image.NewRGBA(w.paper.Rect)
		c.sc = nil
	}
	if c.sc != sc || c.m != m || c.k > k || sigs[c.k] != c.sig {
		copy(c.img.Pix, w.paper.Pix)
		c.sc, c.m, c.k = sc, m, 0
	}
	for i := c.k; i < k; i++ {
		sc.items[i].drawFull(&w.p, c.img, m.Mul(states[i].m), states[i].alpha)
	}
	c.k, c.sig = k, sigs[k]
	copy(dst.Pix, c.img.Pix)

	var (
		tip    geom.Pt
		ink    color.NRGBA
		hasTip bool
	)
	for i := k; i < len(sc.items); i++ {
		st := states[i]
		if st.u <= 0 || st.alpha <= 0 {
			continue
		}
		it := sc.items[i]
		im := m.Mul(st.m)
		if st.u >= 1 || it.anim != "draw" {
			it.drawFull(&w.p, dst, im, st.alpha)
			continue
		}
		if p, cl, ok := it.drawPartial(&w.p, dst, im, st.alpha, st.u); ok {
			tip, ink, hasTip = im.Apply(p), cl, true
		}
	}
	if !marker {
		return
	}
	lift := 0.0
	if !hasTip {
		tip, ink, lift, hasTip = sc.travel(t, m)
	}
	if hasTip {
		w.p.Marker(dst, tip, ink, sc.s720, lift)
	}
}

// travel places the marker between strokes: it glides from where the last
// item ended to where the next one starts, lifted off the board. Over long
// pauses it leaves the frame and comes back.
func (sc *Scene) travel(t float64, m geom.Mat) (geom.Pt, color.NRGBA, float64, bool) {
	prev, next := -1, -1
	for _, i := range sc.marks {
		sp := sc.spans[i]
		if sp.End() <= t {
			if prev < 0 || sp.End() > sc.spans[prev].End() {
				prev = i
			}
		} else if sp.Start > t && next < 0 {
			next = i
		}
	}
	off := geom.Pt{X: float64(sc.w) * 1.06, Y: float64(sc.h) * 1.3}
	at := func(i int, p geom.Pt) geom.Pt {
		return m.Mul(sc.state(i, t).m).Apply(p)
	}
	ink := func(i int) color.NRGBA {
		it := sc.items[i]
		if it.text || len(it.lines) == 0 {
			return it.fill
		}
		return it.stroke
	}
	switch {
	case prev >= 0 && next >= 0 && sc.spans[next].Start-sc.spans[prev].End() <= markerTravel:
		a, b := sc.spans[prev].End(), sc.spans[next].Start
		k := (t - a) / (b - a)
		p := at(prev, sc.items[prev].endTip).Lerp(at(next, sc.items[next].startTip), ease.InOutCubic(k))
		return p, ink(next), math.Sin(math.Pi * k), true
	case next >= 0 && sc.spans[next].Start-t < markerExit:
		k := ease.InOutCubic(1 - (sc.spans[next].Start-t)/markerExit)
		return off.Lerp(at(next, sc.items[next].startTip), k), ink(next), 1 - k, true
	case prev >= 0 && t-sc.spans[prev].End() < markerExit:
		k := ease.InOutCubic((t - sc.spans[prev].End()) / markerExit)
		return at(prev, sc.items[prev].endTip).Lerp(off, k), ink(prev), k, true
	}
	return geom.Pt{}, color.NRGBA{}, 0, false
}

// Still renders the finished drawing seen through view: every element in
// place, motions at rest, highlights shown.
func (sc *Scene) Still(w *worker, view geom.Rect) *image.RGBA {
	img := image.NewRGBA(w.paper.Rect)
	copy(img.Pix, w.paper.Pix)
	m := DeviceMat(view, sc.w, sc.h)
	for _, it := range sc.items {
		it.drawFull(&w.p, img, m, 1)
	}
	return img
}

func newImage(w *worker) *image.RGBA { return image.NewRGBA(w.paper.Rect) }
