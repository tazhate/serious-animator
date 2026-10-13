package svgscene

import (
	"math"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) (*Scene, []Issue) {
	t.Helper()
	sc, issues, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return sc, issues
}

func hasIssue(issues []Issue, isErr bool, substr string) bool {
	for _, i := range issues {
		if i.Error == isErr && strings.Contains(i.Msg, substr) {
			return true
		}
	}
	return false
}

func TestBeatInheritanceAndTransform(t *testing.T) {
	sc, issues := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 900">
  <g data-beat="2" transform="translate(100 50) scale(2)" stroke="#222" stroke-width="3" fill="none">
    <circle cx="10" cy="10" r="5"/>
    <line x1="0" y1="0" x2="10" y2="0" data-beat="3"/>
  </g>
</svg>`)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if len(sc.Items) != 2 {
		t.Fatalf("items: %d", len(sc.Items))
	}
	c, l := sc.Items[0], sc.Items[1]
	if c.Beat != 2 || l.Beat != 3 {
		t.Errorf("beats = %d, %d", c.Beat, l.Beat)
	}
	b := c.Path.Bounds()
	if math.Abs(b.Min.X-110) > 1e-6 || math.Abs(b.Max.Y-80) > 1e-6 {
		t.Errorf("circle bounds %+v", b)
	}
	if c.StrokeWidth != 6 {
		t.Errorf("stroke width %v, want 6", c.StrokeWidth)
	}
	if c.HasFill {
		t.Error("fill=none was not inherited")
	}
}

func TestUnsupportedFeaturesAreErrors(t *testing.T) {
	_, issues := parse(t, `<svg viewBox="0 0 100 100">
  <defs><linearGradient id="g"/></defs>
  <rect x="0" y="0" width="10" height="10" fill="url(#g)" data-beat="1"/>
  <line x1="0" y1="0" x2="5" y2="5" stroke="black" marker-end="url(#a)" data-beat="1"/>
  <image href="x.png"/>
</svg>`)
	for _, want := range []string{"<defs>", "gradients", "arrowheads", "<image>"} {
		if !hasIssue(issues, true, want) {
			t.Errorf("missing error %q in %v", want, issues)
		}
	}
}

func TestOpenPathDefaultFillWarning(t *testing.T) {
	_, issues := parse(t, `<svg viewBox="0 0 100 100"><path d="M0 0 Q50 50 100 0" stroke="black" data-beat="1"/></svg>`)
	if !hasIssue(issues, false, `fill="none"`) {
		t.Fatalf("expected default-fill warning, got %v", issues)
	}
}

func TestStyleAttributeAndColors(t *testing.T) {
	sc, issues := parse(t, `<svg viewBox="0 0 100 100">
  <rect width="10" height="10" style="fill: rgb(255, 0, 0); stroke: #00f; stroke-width: 2px" data-beat="1"/>
  <rect width="10" height="10" fill="#0f08" data-beat="1"/>
</svg>`)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	r := sc.Items[0]
	if r.Fill.R != 255 || r.Stroke.B != 255 || r.StrokeWidth != 2 {
		t.Errorf("style not applied: %+v", r)
	}
	if a := sc.Items[1].Fill.A; a != 0x88 {
		t.Errorf("alpha = %#x", a)
	}
}

func TestTextLayoutCyrillicAndAnchor(t *testing.T) {
	sc, issues := parse(t, `<svg viewBox="0 0 1600 900">
  <text x="800" y="100" font-size="60" text-anchor="middle" data-beat="1">СОЛНЦЕ и вода</text>
</svg>`)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	it := sc.Items[0]
	b := it.Path.Bounds()
	mid := (b.Min.X + b.Max.X) / 2
	if math.Abs(mid-800) > 30 {
		t.Errorf("middle anchor off: %+v", b)
	}
	if b.Max.Y > 100+30 || b.Min.Y < 100-70 {
		t.Errorf("text not on its baseline: %+v", b)
	}
	if it.TextDir.X != 1 {
		t.Errorf("text dir %+v", it.TextDir)
	}
}

func TestBadRoot(t *testing.T) {
	if _, _, err := Parse(strings.NewReader(`<html/>`)); err == nil {
		t.Fatal("expected error for non-svg root")
	}
	if _, _, err := Parse(strings.NewReader(`<svg><g></svg>`)); err == nil {
		t.Fatal("expected xml error")
	}
}

func TestAnimationAttributes(t *testing.T) {
	src := `<svg viewBox="0 0 1600 900">
  <path id="road" data-role="guide" d="M100 800 Q800 200 1500 800" fill="none"/>
  <g id="gear" data-beat="1" data-spin="90" data-fill="solid" data-highlight="circle@2">
    <circle cx="300" cy="300" r="80" fill="#a5d8ff" stroke="#222"/>
    <text x="300" y="450" font-size="40" text-anchor="middle" data-nodim="">GEAR</text>
  </g>
  <g data-beat="2" data-move="#road" data-orient="auto" data-anim="pop">
    <rect x="80" y="780" width="40" height="40" fill="#b2f2bb"/>
  </g>
  <path data-beat="2" data-wobble="3" d="M0 0 L10 10" fill="none" stroke="#222"/>
</svg>`
	d, issues, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var errs []string
	for _, is := range issues {
		if is.Error {
			errs = append(errs, is.Msg)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "data-wobble") {
		t.Fatalf("want one error about data-wobble, got %q", errs)
	}
	if len(d.Items) != 4 {
		t.Fatalf("guide must not be drawn: %d items", len(d.Items))
	}
	c, txt, car := d.Items[0], d.Items[1], d.Items[2]
	if c.FillStyle != "solid" || c.Motion != 1 || txt.Motion != 1 || !txt.NoDim || c.NoDim {
		t.Fatalf("inheritance: %+v / %+v", c, txt)
	}
	if car.Anim != "pop" || car.Motion != 2 {
		t.Fatalf("car: %+v", car)
	}
	if len(txt.Glyphs) != 5 || txt.Glyphs[0] >= txt.Glyphs[4] {
		t.Fatalf("glyph stops %v", txt.Glyphs)
	}
	m := d.Motions[0]
	if m.Spin != 90 || math.Abs(m.Origin.X-300) > 1 {
		t.Fatalf("spin motion %+v", m)
	}
	if mv := d.Motions[1]; mv.Path == nil || !mv.Orient || mv.Dur != 2 {
		t.Fatalf("move motion %+v", mv)
	}
	if len(d.Highlights) != 1 || d.Highlights[0].Beat != 2 || d.Highlights[0].Target.Empty() {
		t.Fatalf("highlights %+v", d.Highlights)
	}
	if _, ok := d.Boxes["gear"]; !ok {
		t.Fatal("group box not recorded")
	}
}
