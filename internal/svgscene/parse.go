// Package svgscene parses the SVG subset that scene drawings use and turns it
// into a flat, ordered list of drawable items, each tied to a narration beat.
package svgscene

import (
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/tazhate/serious-animator/internal/geom"
)

// Item is one drawable element, in document (= drawing) order. Geometry is
// already transformed into viewBox coordinates.
type Item struct {
	ID   string
	Tag  string
	Line int
	Beat int

	Path geom.Path

	HasStroke   bool
	Stroke      color.NRGBA
	StrokeWidth float64 // viewBox units

	HasFill bool
	Fill    color.NRGBA

	// Text items only.
	Text    string
	TextDir geom.Pt   // unit vector along the baseline, in viewBox space
	Glyphs  []float64 // glyph boundaries projected on TextDir, viewBox units

	FillStyle string // data-fill: "" (renderer decides), "hachure" or "solid"
	Anim      string // data-anim: "" or "draw", "pop", "fade"
	NoDim     bool   // data-nodim: stays bright when the scene dims old beats
	Motion    int    // 1-based index into Scene.Motions, 0 when static
}

// IsText reports whether the item came from a <text> element.
func (it *Item) IsText() bool { return it.Tag == "text" }

// Bounds returns the item's box including half the stroke width.
func (it *Item) Bounds() geom.Rect {
	b := it.Path.Bounds()
	if it.HasStroke {
		b = b.Inset(it.StrokeWidth / 2)
	}
	return b
}

// Scene is a parsed drawing.
type Scene struct {
	ViewBox    geom.Rect
	Items      []Item
	Highlights []Highlight
	Motions    []Motion
	// Guides are invisible paths (data-role="guide") that motions follow,
	// by id, in viewBox coordinates.
	Guides map[string]geom.Path
	// Boxes are the bounds of every element and group with an id.
	Boxes map[string]geom.Rect
}

// Highlight draws attention to an element during a later beat: a hand-drawn
// circle around it or a line under it.
type Highlight struct {
	Kind   string // "circle" or "underline"
	Beat   int
	Target geom.Rect
	Line   int
}

// Motion moves a group once it is drawn: it spins around Origin, slides by
// Delta, or travels along a guide path.
type Motion struct {
	Spin   float64 // degrees per second, clockwise on screen
	Origin geom.Pt
	Guide  string    // guide id for data-move="#id"
	Path   geom.Path // the resolved guide
	Delta  geom.Pt
	Dur    float64 // seconds for a move
	Orient bool    // turn with the guide's direction
	Beat   int     // start at this beat instead of when the group is drawn
	Bounds geom.Rect
	Line   int
}

// KnownData lists the data-* attributes the renderer understands.
var KnownData = []string{
	"data-beat", "data-fill", "data-anim", "data-nodim", "data-highlight", "data-role",
	"data-spin", "data-origin", "data-move", "data-move-dur", "data-orient", "data-motion-beat",
}

// Issue is a problem found while parsing. Errors make the scene unusable,
// warnings point at likely mistakes.
type Issue struct {
	Line  int
	Error bool
	Msg   string
}

func (i Issue) String() string {
	lvl := "warning"
	if i.Error {
		lvl = "error"
	}
	return fmt.Sprintf("line %d: %s: %s", i.Line, lvl, i.Msg)
}

type state struct {
	ctm           geom.Mat
	fill          paint
	fillSet       bool
	stroke        paint
	strokeWidth   float64
	opacity       float64
	fillOpacity   float64
	strokeOpacity float64
	fontSize      float64
	anchor        string
	beat          int
	fillStyle     string
	anim          string
	noDim         bool
	motion        int
}

// frame tracks an open element: where its items start and what has to be
// done once all of them are known.
type frame struct {
	start  int
	id     string
	hl     []Highlight
	motion int // motion created by this element, 1-based
}

// unsupported maps element names to the hint shown to the author.
var unsupported = map[string]string{
	"defs":           "<defs> is not supported, inline every shape",
	"use":            "<use> is not supported, repeat the shape",
	"symbol":         "<symbol> is not supported, inline the shape",
	"style":          "<style> is not supported, use attributes",
	"script":         "<script> is not allowed",
	"image":          "<image> is not supported, draw it with paths",
	"foreignObject":  "<foreignObject> is not supported",
	"filter":         "filters are not supported",
	"mask":           "masks are not supported",
	"clipPath":       "clip paths are not supported",
	"pattern":        "patterns are not supported, use flat fills",
	"linearGradient": "gradients are not supported, use flat fills",
	"radialGradient": "gradients are not supported, use flat fills",
	"marker":         "markers are not supported, draw arrowheads as paths",
	"svg":            "nested <svg> is not supported, use <g transform>",
}

var ignored = map[string]bool{"title": true, "desc": true, "metadata": true}

// Parse reads an SVG document. The returned error is only for unreadable XML
// or a missing root; everything else is reported as Issues.
func Parse(r io.Reader) (*Scene, []Issue, error) {
	p := &parser{dec: xml.NewDecoder(r)}
	p.dec.Strict = true
	if err := p.run(); err != nil {
		return nil, p.issues, err
	}
	return p.scene, p.issues, nil
}

type parser struct {
	dec    *xml.Decoder
	scene  *Scene
	issues []Issue
	stack  []state
	frames []frame
}

func (p *parser) line() int {
	l, _ := p.dec.InputPos()
	return l
}

func (p *parser) errf(line int, format string, a ...any) {
	p.issues = append(p.issues, Issue{Line: line, Error: true, Msg: fmt.Sprintf(format, a...)})
}

func (p *parser) warnf(line int, format string, a ...any) {
	p.issues = append(p.issues, Issue{Line: line, Msg: fmt.Sprintf(format, a...)})
}

func (p *parser) run() error {
	for {
		tok, err := p.dec.Token()
		if errors.Is(err, io.EOF) {
			if p.scene == nil {
				return errors.New("no <svg> root element")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "svg" {
			return fmt.Errorf("root element is <%s>, want <svg>", se.Name.Local)
		}
		if err := p.root(se); err != nil {
			return err
		}
		if err := p.children(); err != nil {
			return err
		}
		p.resolveGuides()
		return nil
	}
}

func (p *parser) root(se xml.StartElement) error {
	line := p.line()
	a := attrs(se)
	var vb geom.Rect
	if v, ok := a["viewBox"]; ok {
		n, err := parseNumbers(v)
		if err != nil || len(n) != 4 || n[2] <= 0 || n[3] <= 0 {
			return fmt.Errorf("line %d: bad viewBox %q", line, v)
		}
		vb = geom.Rect{Min: geom.Pt{X: n[0], Y: n[1]}, Max: geom.Pt{X: n[0] + n[2], Y: n[1] + n[3]}}
	} else {
		w, err1 := parseLength(a["width"])
		h, err2 := parseLength(a["height"])
		if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
			return fmt.Errorf("line %d: <svg> needs a viewBox", line)
		}
		vb = geom.Rect{Max: geom.Pt{X: w, Y: h}}
	}
	p.scene = &Scene{ViewBox: vb, Guides: map[string]geom.Path{}, Boxes: map[string]geom.Rect{}}
	base := state{
		ctm:           geom.Identity,
		fill:          paint{c: color.NRGBA{0, 0, 0, 255}},
		stroke:        paint{none: true},
		strokeWidth:   1,
		opacity:       1,
		fillOpacity:   1,
		strokeOpacity: 1,
		fontSize:      16,
		anchor:        "start",
	}
	p.stack = []state{p.inherit(base, a, line)}
	p.frames = []frame{{}}
	return nil
}

// children walks the content of the root element until its end tag.
func (p *parser) children() error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			p.stack = p.stack[:len(p.stack)-1]
			p.closeFrame(p.frames[len(p.frames)-1])
			p.frames = p.frames[:len(p.frames)-1]
			if len(p.stack) == 0 {
				return nil
			}
		case xml.StartElement:
			if err := p.element(t); err != nil {
				return err
			}
		}
	}
}

func (p *parser) element(se xml.StartElement) error {
	line := p.line()
	name := se.Name.Local
	if hint, bad := unsupported[name]; bad {
		p.errf(line, "%s", hint)
		return p.dec.Skip()
	}
	if ignored[name] {
		return p.dec.Skip()
	}
	a := attrs(se)
	parent := p.stack[len(p.stack)-1]
	st := p.inherit(parent, a, line)
	fr := frame{start: len(p.scene.Items), id: a["id"], hl: p.highlights(a, line)}
	if m, ok := p.motion(st, parent, a, line); ok {
		p.scene.Motions = append(p.scene.Motions, m)
		st.motion = len(p.scene.Motions)
		fr.motion = st.motion
	}
	switch name {
	case "g", "a":
		p.stack = append(p.stack, st)
		p.frames = append(p.frames, fr)
		return nil
	case "text":
		text, err := p.textContent()
		if err != nil {
			return err
		}
		p.addText(st, a, text, line)
		p.closeFrame(fr)
		return nil
	}
	path, err := shapePath(name, a)
	switch {
	case err != nil:
		p.errf(line, "<%s>: %v", name, err)
	case path == nil:
		p.warnf(line, "<%s> is not a supported element, skipped", name)
	case a["data-role"] == "guide":
		if a["id"] == "" {
			p.errf(line, "a guide needs an id, so data-move can name it")
		} else {
			p.scene.Guides[a["id"]] = path.Transform(st.ctm)
		}
	default:
		p.addShape(st, a, name, path, line)
	}
	p.closeFrame(fr)
	return p.dec.Skip()
}

// closeFrame finishes an element once its items are known: it records the
// element's box and turns its highlights and motion into scene entries.
func (p *parser) closeFrame(fr frame) {
	items := p.scene.Items[min(fr.start, len(p.scene.Items)):]
	box := geom.EmptyRect()
	for i := range items {
		box = box.Union(items[i].Bounds())
	}
	if fr.id != "" && !box.Empty() {
		p.scene.Boxes[fr.id] = box
	}
	for _, h := range fr.hl {
		if box.Empty() {
			p.errf(h.Line, "data-highlight is on an element that draws nothing")
			continue
		}
		h.Target = box
		p.scene.Highlights = append(p.scene.Highlights, h)
	}
	if fr.motion > 0 {
		m := &p.scene.Motions[fr.motion-1]
		m.Bounds = box
		if math.IsNaN(m.Origin.X) {
			m.Origin = geom.Pt{X: (box.Min.X + box.Max.X) / 2, Y: (box.Min.Y + box.Max.Y) / 2}
		}
	}
}

// highlights parses data-highlight="circle@4 underline@5".
func (p *parser) highlights(a map[string]string, line int) []Highlight {
	v, ok := a["data-highlight"]
	if !ok {
		return nil
	}
	var out []Highlight
	for f := range strings.FieldsSeq(v) {
		kind, beat, found := strings.Cut(f, "@")
		n, err := strconv.Atoi(beat)
		if !found || err != nil || n < 1 || (kind != "circle" && kind != "underline") {
			p.errf(line, "data-highlight: %q must look like circle@N or underline@N", f)
			continue
		}
		out = append(out, Highlight{Kind: kind, Beat: n, Line: line})
	}
	return out
}

// motion parses the motion attributes of one element.
func (p *parser) motion(st, parent state, a map[string]string, line int) (Motion, bool) {
	_, spin := a["data-spin"]
	_, move := a["data-move"]
	if !spin && !move {
		for _, k := range []string{"data-origin", "data-move-dur", "data-orient", "data-motion-beat"} {
			if _, ok := a[k]; ok {
				p.errf(line, "%s needs data-spin or data-move on the same element", k)
			}
		}
		return Motion{}, false
	}
	if parent.motion > 0 {
		p.errf(line, "motions cannot be nested: a parent group already moves")
		return Motion{}, false
	}
	m := Motion{Origin: geom.Pt{X: math.NaN()}, Dur: 2, Line: line}
	if v, ok := a["data-spin"]; ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || math.Abs(f) > 720 {
			p.errf(line, "data-spin: %q must be degrees per second, at most 720", v)
		}
		m.Spin = f
	}
	if v, ok := a["data-origin"]; ok {
		n, err := parseNumbers(v)
		if err != nil || len(n) != 2 {
			p.errf(line, "data-origin: %q must be \"x,y\"", v)
		} else {
			m.Origin = st.ctm.Apply(geom.Pt{X: n[0], Y: n[1]})
		}
	}
	if v, ok := a["data-move"]; ok {
		if id, isRef := strings.CutPrefix(strings.TrimSpace(v), "#"); isRef {
			m.Guide = id
		} else if n, err := parseNumbers(v); err == nil && len(n) == 2 {
			m.Delta = st.ctm.Apply(geom.Pt{X: n[0], Y: n[1]}).Sub(st.ctm.Apply(geom.Pt{}))
		} else {
			p.errf(line, "data-move: %q must be \"#guide-id\" or \"dx,dy\"", v)
		}
	}
	if v, ok := a["data-move-dur"]; ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || f <= 0 || f > 30 {
			p.errf(line, "data-move-dur: %q must be seconds between 0 and 30", v)
		} else {
			m.Dur = f
		}
	}
	if v, ok := a["data-orient"]; ok {
		if v != "auto" {
			p.errf(line, "data-orient: only \"auto\" is supported")
		}
		m.Orient = true
	}
	if v, ok := a["data-motion-beat"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 1 {
			p.errf(line, "data-motion-beat: %q must be a positive integer", v)
		} else {
			m.Beat = n
		}
	}
	return m, true
}

// resolveGuides attaches guide paths to the motions that follow them.
func (p *parser) resolveGuides() {
	for i := range p.scene.Motions {
		m := &p.scene.Motions[i]
		if m.Guide == "" {
			continue
		}
		g, ok := p.scene.Guides[m.Guide]
		if !ok {
			p.errf(m.Line, "data-move: no guide with id %q (give a path data-role=\"guide\" and that id)", m.Guide)
			continue
		}
		m.Path = g
	}
}

// textContent collects character data of a <text>, tspans included, and
// consumes the closing tag.
func (p *parser) textContent() (string, error) {
	var b strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := p.dec.Token()
		if err != nil {
			return "", fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "tspan" {
				for _, at := range t.Attr {
					if at.Name.Local == "x" || at.Name.Local == "y" || at.Name.Local == "dy" || at.Name.Local == "dx" {
						p.warnf(p.line(), "<tspan> positioning is ignored, use one <text> per line")
						break
					}
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(t)
		}
	}
	return strings.Join(strings.Fields(b.String()), " "), nil
}

func attrs(se xml.StartElement) map[string]string {
	m := make(map[string]string, len(se.Attr))
	for _, a := range se.Attr {
		key := a.Name.Local
		if a.Name.Space != "" && a.Name.Space != "http://www.w3.org/2000/svg" {
			// Keep data-* and plain attributes; drop namespaced ones (xlink, xml).
			continue
		}
		m[key] = a.Value
	}
	if s, ok := m["style"]; ok {
		maps.Copy(m, parseStyleAttr(s)) // style wins over presentation attributes, as in CSS
	}
	return m
}

func (p *parser) inherit(parent state, a map[string]string, line int) state {
	st := parent
	if v, ok := a["transform"]; ok {
		m, err := parseTransform(v)
		if err != nil {
			p.errf(line, "%v", err)
		} else {
			st.ctm = st.ctm.Mul(m)
		}
	}
	if v, ok := a["fill"]; ok {
		pt, err := parsePaint(v)
		if err != nil {
			p.errf(line, "fill: %v", err)
		} else {
			st.fill, st.fillSet = pt, true
		}
	}
	if v, ok := a["stroke"]; ok {
		pt, err := parsePaint(v)
		if err != nil {
			p.errf(line, "stroke: %v", err)
		} else {
			st.stroke = pt
		}
	}
	num := func(key string, dst *float64, lo float64) {
		v, ok := a[key]
		if !ok {
			return
		}
		f, err := parseLength(v)
		if err != nil || f < lo {
			p.errf(line, "%s: bad value %q", key, v)
			return
		}
		*dst = f
	}
	num("stroke-width", &st.strokeWidth, 0)
	num("font-size", &st.fontSize, 0.1)
	var op float64 = 1
	num("opacity", &op, 0)
	st.opacity *= min(op, 1)
	num("fill-opacity", &st.fillOpacity, 0)
	num("stroke-opacity", &st.strokeOpacity, 0)
	if v, ok := a["text-anchor"]; ok {
		st.anchor = v
	}
	if v, ok := a["data-beat"]; ok {
		b, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || b < 1 {
			p.errf(line, "data-beat must be a positive integer, got %q", v)
		} else {
			st.beat = b
		}
	}
	if v, ok := a["data-fill"]; ok {
		if v != "hachure" && v != "solid" {
			p.errf(line, "data-fill: %q must be hachure or solid", v)
		} else {
			st.fillStyle = v
		}
	}
	if v, ok := a["data-anim"]; ok {
		if v != "draw" && v != "pop" && v != "fade" {
			p.errf(line, "data-anim: %q must be draw, pop or fade", v)
		} else {
			st.anim = v
		}
	}
	if _, ok := a["data-nodim"]; ok {
		st.noDim = true
	}
	if v, ok := a["data-role"]; ok && v != "guide" {
		p.errf(line, "data-role: only \"guide\" is supported")
	}
	for k := range a {
		if strings.HasPrefix(k, "data-") && !slices.Contains(KnownData, k) {
			p.errf(line, "unknown attribute %s; known: %s", k, strings.Join(KnownData, ", "))
		}
	}
	if _, ok := a["marker-end"]; ok {
		p.errf(line, "markers are not supported, draw arrowheads as paths")
	}
	if _, ok := a["marker-start"]; ok {
		p.errf(line, "markers are not supported, draw arrowheads as paths")
	}
	if _, ok := a["stroke-dasharray"]; ok {
		p.warnf(line, "stroke-dasharray is ignored, lines are drawn solid")
	}
	return st
}

func shapePath(name string, a map[string]string) (geom.Path, error) {
	f := func(key string) (float64, error) {
		v, ok := a[key]
		if !ok {
			return 0, nil
		}
		return parseLength(v)
	}
	var errs []error
	g := func(key string) float64 {
		v, err := f(key)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return v
	}
	var path geom.Path
	switch name {
	case "path":
		d := strings.TrimSpace(a["d"])
		if d == "" {
			return nil, errors.New("empty d attribute")
		}
		pp, err := geom.ParsePath(d)
		if err != nil {
			return nil, err
		}
		path = pp
	case "rect":
		x, y, w, h := g("x"), g("y"), g("width"), g("height")
		rx, ry := g("rx"), g("ry")
		_, hasRx := a["rx"]
		_, hasRy := a["ry"]
		if hasRx && !hasRy {
			ry = rx
		} else if hasRy && !hasRx {
			rx = ry
		}
		if w <= 0 || h <= 0 {
			return nil, errors.New("width and height must be positive")
		}
		path = geom.RectPath(x, y, w, h, rx, ry)
	case "circle":
		r := g("r")
		if r <= 0 {
			return nil, errors.New("r must be positive")
		}
		path = geom.EllipsePath(g("cx"), g("cy"), r, r)
	case "ellipse":
		rx, ry := g("rx"), g("ry")
		if rx <= 0 || ry <= 0 {
			return nil, errors.New("rx and ry must be positive")
		}
		path = geom.EllipsePath(g("cx"), g("cy"), rx, ry)
	case "line":
		path = geom.PolyPath([]geom.Pt{{X: g("x1"), Y: g("y1")}, {X: g("x2"), Y: g("y2")}}, false)
	case "polyline", "polygon":
		n, err := parseNumbers(a["points"])
		if err != nil {
			return nil, err
		}
		if len(n) < 4 || len(n)%2 != 0 {
			return nil, errors.New("points needs at least two x,y pairs")
		}
		pts := make([]geom.Pt, len(n)/2)
		for i := range pts {
			pts[i] = geom.Pt{X: n[2*i], Y: n[2*i+1]}
		}
		path = geom.PolyPath(pts, name == "polygon")
	default:
		return nil, nil
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return path, nil
}

func (p *parser) nextID(a map[string]string, tag string, line int) string {
	if id := a["id"]; id != "" {
		return id
	}
	return fmt.Sprintf("%s@%d", tag, line)
}

func withAlpha(c color.NRGBA, k float64) color.NRGBA {
	c.A = uint8(float64(c.A) * min(max(k, 0), 1))
	return c
}

func (p *parser) addShape(st state, a map[string]string, tag string, path geom.Path, line int) {
	it := Item{
		ID:        p.nextID(a, tag, line),
		Tag:       tag,
		Line:      line,
		Beat:      st.beat,
		Path:      path.Transform(st.ctm),
		FillStyle: st.fillStyle,
		Anim:      st.anim,
		NoDim:     st.noDim,
		Motion:    st.motion,
	}
	if !st.stroke.none && st.strokeWidth > 0 {
		it.HasStroke = true
		it.Stroke = withAlpha(st.stroke.c, st.opacity*st.strokeOpacity)
		it.StrokeWidth = st.strokeWidth * st.ctm.Scale()
	}
	open := tag == "line" || tag == "polyline"
	if tag == "path" {
		for _, s := range path {
			if !s.Closed {
				open = true
			}
		}
	}
	if !st.fill.none && tag != "line" {
		it.HasFill = true
		it.Fill = withAlpha(st.fill.c, st.opacity*st.fillOpacity)
		if open && !st.fillSet && it.HasStroke {
			p.warnf(line, "<%s> is open and has no fill attribute, so SVG fills it black; add fill=\"none\"", tag)
		}
	}
	if !it.HasFill && !it.HasStroke {
		p.warnf(line, "<%s> has neither fill nor stroke and is invisible", tag)
		return
	}
	p.scene.Items = append(p.scene.Items, it)
}

func (p *parser) addText(st state, a map[string]string, text string, line int) {
	if text == "" {
		p.warnf(line, "empty <text>, skipped")
		return
	}
	x, _ := parseLength(firstValue(a["x"]))
	y, _ := parseLength(firstValue(a["y"]))
	local, stops, err := TextGlyphs(text, st.fontSize, st.anchor)
	if err != nil {
		p.errf(line, "text: %v", err)
		return
	}
	m := st.ctm.Mul(geom.Translate(x, y))
	dir := m.Apply(geom.Pt{X: 1}).Sub(m.Apply(geom.Pt{}))
	if l := dir.Len(); l > 0 {
		dir = dir.Mul(1 / l)
	}
	glyphs := make([]float64, len(stops))
	for i, x := range stops {
		q := m.Apply(geom.Pt{X: x})
		glyphs[i] = q.X*dir.X + q.Y*dir.Y
	}
	it := Item{
		ID:        p.nextID(a, "text", line),
		Tag:       "text",
		Line:      line,
		Beat:      st.beat,
		Path:      local.Transform(m),
		Text:      text,
		TextDir:   dir,
		Glyphs:    glyphs,
		FillStyle: "solid",
		Anim:      st.anim,
		NoDim:     st.noDim,
		Motion:    st.motion,
	}
	if !st.fill.none {
		it.HasFill = true
		it.Fill = withAlpha(st.fill.c, st.opacity*st.fillOpacity)
	}
	if !st.stroke.none && st.strokeWidth > 0 {
		it.HasStroke = true
		it.Stroke = withAlpha(st.stroke.c, st.opacity*st.strokeOpacity)
		it.StrokeWidth = st.strokeWidth * m.Scale()
	}
	if !it.HasFill && !it.HasStroke {
		p.warnf(line, "<text> has neither fill nor stroke and is invisible")
		return
	}
	p.scene.Items = append(p.scene.Items, it)
}

func firstValue(v string) string {
	if v == "" {
		return "0"
	}
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
	if len(f) == 0 {
		return "0"
	}
	return f[0]
}
