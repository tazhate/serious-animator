package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strconv"

	"github.com/tazhate/serious-animator/internal/audio"
	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/project"
	"github.com/tazhate/serious-animator/internal/raster"
	"github.com/tazhate/serious-animator/internal/svgscene"
)

// beatColors tell beats apart in the preview overlay.
var beatColors = []color.NRGBA{
	{0xe6, 0x39, 0x46, 0xff}, {0x1d, 0x80, 0xc2, 0xff}, {0x2a, 0x9d, 0x4b, 0xff}, {0xf4, 0xa2, 0x61, 0xff},
	{0x8e, 0x44, 0xad, 0xff}, {0x16, 0xa0, 0x85, 0xff}, {0xd3, 0x54, 0x00, 0xff}, {0x34, 0x49, 0x5e, 0xff},
}

// PreviewOptions control Preview.
type PreviewOptions struct {
	Quality   Quality
	Roughness float64
	Beats     bool // outline and number each beat's area
	Frames    bool // also write what the camera shows at every beat
}

// Preview writes the finished drawing of every scene as PNG into dir and
// returns the file paths. With Beats, each beat's area is outlined and
// numbered so the drawing order can be checked at a glance; camera scenes
// also get the view of every beat outlined in gray. With Frames, camera
// scenes get one image per beat as the viewer will see it.
func Preview(proj *project.Project, dir string, o PreviewOptions) ([]string, error) {
	var paths []string
	write := func(name string, img image.Image) error {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if err := audio.WriteFileAtomic(path, buf.Bytes()); err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}
	cfgs := Configs(proj, o.Quality, o.Roughness)
	w := newWorker(raster.Paper(o.Quality.W, o.Quality.H, 1))
	for si, sc := range proj.Board.Scenes {
		d := proj.Drawings[si]
		if d == nil {
			return paths, fmt.Errorf("scene %s has no drawing", sc.ID)
		}
		ps := PrepareScene(d, cfgs[si])
		img := ps.Still(w, ps.vb)
		views, _, _ := ps.beatViews()
		if o.Beats {
			m := DeviceMat(ps.vb, ps.w, ps.h)
			if ps.cam {
				for _, v := range views {
					drawBox(img, m, v, color.NRGBA{0x80, 0x80, 0x80, 0x90}, ps.s720, 1)
				}
			}
			drawBeatOverlay(img, ps, m, len(sc.Beats))
		}
		if err := write(sc.ID+".png", img); err != nil {
			return paths, err
		}
		if o.Frames && ps.cam {
			for b, v := range views {
				if err := write(fmt.Sprintf("%s-b%d.png", sc.ID, b+1), ps.Still(w, v)); err != nil {
					return paths, err
				}
			}
		}
	}
	return paths, nil
}

// beatBoxes returns, per beat (index 0 = beat 1), the viewBox-space box of
// everything drawn in that beat.
func (sc *Scene) beatBoxes(beats int) []geom.Rect {
	out := make([]geom.Rect, beats)
	for i := range out {
		out[i] = geom.EmptyRect()
	}
	for _, it := range sc.items {
		if b := it.src.Beat; b >= 1 && b <= beats {
			out[b-1] = out[b-1].Union(it.bounds)
		}
	}
	return out
}

func drawBox(img *image.RGBA, m geom.Mat, r geom.Rect, c color.NRGBA, s720, pad float64) {
	var p raster.Painter
	a, b := m.Apply(r.Min), m.Apply(r.Max)
	x0, y0, x1, y1 := a.X-pad*s720, a.Y-pad*s720, b.X+pad*s720, b.Y+pad*s720
	polys := raster.StrokePolys(nil, []geom.Pt{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}, {X: x0, Y: y0}}, max(2*s720, 1.5))
	p.Fill(img, polys, c)
}

func drawBeatOverlay(img *image.RGBA, ps *Scene, m geom.Mat, beats int) {
	var p raster.Painter
	for bi, r := range ps.beatBoxes(beats) {
		if r.Empty() {
			continue
		}
		c := beatColors[bi%len(beatColors)]
		c.A = 0xcc
		drawBox(img, m, r, c, ps.s720, 4)

		// Number badge at the top-left corner.
		rad := 14 * ps.s720
		corner := m.Apply(r.Min)
		center := geom.Pt{X: corner.X - 4*ps.s720, Y: corner.Y - 4*ps.s720}
		p.Fill(img, [][]geom.Pt{geom.EllipsePath(center.X, center.Y, rad, rad).Flatten(0.3)[0].Pts}, c)
		label, err := svgscene.TextPath(strconv.Itoa(bi+1), 22*ps.s720, "middle")
		if err != nil {
			continue
		}
		label = label.Transform(geom.Translate(center.X, center.Y+7*ps.s720))
		var lp [][]geom.Pt
		for _, pl := range label.Flatten(0.2) {
			lp = append(lp, pl.Pts)
		}
		p.Fill(img, lp, color.White)
	}
}
