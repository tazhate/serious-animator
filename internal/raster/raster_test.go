package raster

import (
	"bytes"
	"testing"

	"github.com/tazhate/serious-animator/internal/geom"
)

func TestPaperIsDeterministicAndWarm(t *testing.T) {
	a, b := Paper(64, 36, 7), Paper(64, 36, 7)
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("paper differs between calls")
	}
	c := a.RGBAAt(32, 18)
	if c.R <= c.B || c.R < 240 {
		t.Fatalf("center %v is not warm off-white", c)
	}
}

func TestTaperedStrokeFollowsWidths(t *testing.T) {
	pts := []geom.Pt{{X: 0, Y: 10}, {X: 50, Y: 10}, {X: 100, Y: 10}}
	polys := TaperedStrokePolys(nil, pts, []float64{2, 10, 4})
	r := geom.EmptyRect()
	for _, p := range polys {
		for _, q := range p {
			r = r.Extend(q)
		}
	}
	if r.Max.Y-10 < 4.9 || r.Max.Y-10 > 5.1 {
		t.Fatalf("max half width %v, want 5", r.Max.Y-10)
	}
}
