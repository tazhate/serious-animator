package svgscene

import (
	"fmt"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/tazhate/serious-animator/assets"
	"github.com/tazhate/serious-animator/internal/geom"
)

// TextPath converts a single line of text into glyph outlines with the
// baseline at y=0 and the anchor point at x=0.
func TextPath(text string, size float64, anchor string) (geom.Path, error) {
	path, _, err := TextGlyphs(text, size, anchor)
	return path, err
}

// TextGlyphs is TextPath that also returns the x position of every glyph
// boundary along the baseline: len(text runes)+1 values, first and last at
// the ends of the line.
func TextGlyphs(text string, size float64, anchor string) (geom.Path, []float64, error) {
	f, err := assets.Font()
	if err != nil {
		return nil, nil, fmt.Errorf("load font: %w", err)
	}
	var buf sfnt.Buffer
	upem := float64(f.UnitsPerEm())
	ppem := fixed.I(int(upem)) // load outlines in font units
	scale := size / upem

	var path geom.Path
	x := 0.0
	stops := []float64{0}
	var missing []rune
	prev := sfnt.GlyphIndex(0)
	for i, r := range text {
		gi, err := f.GlyphIndex(&buf, r)
		if err != nil {
			return nil, nil, err
		}
		if gi == 0 && r != ' ' {
			missing = append(missing, r)
		}
		if i > 0 && prev != 0 && gi != 0 {
			if k, err := f.Kern(&buf, prev, gi, ppem, font.HintingNone); err == nil {
				x += float64(k) / 64 * scale
			}
		}
		segs, err := f.LoadGlyph(&buf, gi, ppem, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("glyph %q: %w", r, err)
		}
		path = appendGlyph(path, segs, x, scale)
		adv, err := f.GlyphAdvance(&buf, gi, ppem, font.HintingNone)
		if err != nil {
			return nil, nil, fmt.Errorf("advance %q: %w", r, err)
		}
		x += float64(adv) / 64 * scale
		stops = append(stops, x)
		prev = gi
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("font has no glyphs for %q", string(missing))
	}
	shift := 0.0
	switch anchor {
	case "middle":
		shift = -x / 2
	case "end":
		shift = -x
	}
	if shift != 0 {
		path = path.Transform(geom.Translate(shift, 0))
		for i := range stops {
			stops[i] += shift
		}
	}
	return path, stops, nil
}

// appendGlyph converts sfnt segments (26.6, y down) into path subpaths.
func appendGlyph(path geom.Path, segs sfnt.Segments, dx, scale float64) geom.Path {
	pt := func(p fixed.Point26_6) geom.Pt {
		return geom.Pt{X: dx + float64(p.X)/64*scale, Y: float64(p.Y) / 64 * scale}
	}
	var cur *geom.Sub
	last := geom.Pt{}
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			path = append(path, geom.Sub{Start: pt(s.Args[0]), Closed: true})
			cur = &path[len(path)-1]
			last = cur.Start
		case sfnt.SegmentOpLineTo:
			p := pt(s.Args[0])
			cur.Segs = append(cur.Segs, geom.Seg{Kind: geom.Line, P: p})
			last = p
		case sfnt.SegmentOpQuadTo:
			q, p := pt(s.Args[0]), pt(s.Args[1])
			cur.Segs = append(cur.Segs, geom.Seg{
				Kind: geom.Cubic,
				C1:   last.Add(q.Sub(last).Mul(2.0 / 3)),
				C2:   p.Add(q.Sub(p).Mul(2.0 / 3)),
				P:    p,
			})
			last = p
		case sfnt.SegmentOpCubeTo:
			p := pt(s.Args[2])
			cur.Segs = append(cur.Segs, geom.Seg{Kind: geom.Cubic, C1: pt(s.Args[0]), C2: pt(s.Args[1]), P: p})
			last = p
		}
	}
	return path
}
