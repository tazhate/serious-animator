package render

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/project"
	"github.com/tazhate/serious-animator/internal/raster"
	"github.com/tazhate/serious-animator/internal/svgscene"
	"github.com/tazhate/serious-animator/internal/timeline"
)

const testSVG = `<svg viewBox="0 0 1600 900">
  <g data-beat="1" stroke="#222" stroke-width="6">
    <text x="800" y="120" font-size="70" text-anchor="middle" fill="#222" stroke="none">ЗАГОЛОВОК</text>
    <circle cx="400" cy="450" r="120" fill="#f4d35e"/>
  </g>
  <g data-beat="2" stroke="#222" stroke-width="6" fill="none">
    <path d="M550 450 Q800 300 1050 450"/>
    <path d="M1020 425 L1050 450 L1015 465"/>
    <rect x="1080" y="350" width="200" height="200" rx="12" fill="#8ecae6"/>
  </g>
</svg>`

func parseSVG(t testing.TB, src string) *svgscene.Scene {
	t.Helper()
	d, issues, err := svgscene.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issues {
		if is.Error {
			t.Fatal(is)
		}
	}
	return d
}

func clipsOf(n ...[]int) []timeline.SceneClips {
	out := make([]timeline.SceneClips, len(n))
	for i, sc := range n {
		for _, c := range sc {
			out[i].Clips = append(out[i].Clips, timeline.Clip{Samples: c, Anchor: -1})
		}
	}
	return out
}

func planned(t testing.TB, src string, w, h int, cfg SceneConfig, clips ...[]int) (*Scene, timeline.Plan) {
	t.Helper()
	cfg.W, cfg.H = w, h
	if cfg.ID == "" {
		cfg.ID = "s01"
	}
	sc := PrepareScene(parseSVG(t, src), cfg)
	plan := timeline.Build(clipsOf(clips...), 24000, timeline.Default)
	sc.Plan(plan.Scenes[0])
	return sc, plan
}

func TestLastFrameIsTheFinishedDrawing(t *testing.T) {
	sc, plan := planned(t, testSVG, 320, 180, SceneConfig{Roughness: 1}, []int{24000, 24000})
	ts := plan.Scenes[0]
	for i, sp := range sc.spans {
		if sp.End() > ts.Beats[sc.items[i].src.Beat-1].DrawEnd+1e-9 {
			t.Fatalf("item %d ends at %v, after its beat window", i, sp.End())
		}
	}
	w := newWorker(raster.Paper(320, 180, 1))
	frame := func(at float64) []byte {
		img := newImage(w)
		sc.frame(w, at, 0, true, img)
		return img.Pix
	}
	mid := frame(ts.Beats[1].ClipStart + 0.2)
	last := frame(ts.Dur - 1e-3)
	still := sc.Still(w, sc.vb)
	if !bytes.Equal(last, still.Pix) {
		t.Fatal("last frame differs from the static drawing")
	}
	if bytes.Equal(mid, still.Pix) {
		t.Fatal("mid-scene frame already shows the finished drawing")
	}
}

func TestArrowHeadPops(t *testing.T) {
	sc := PrepareScene(parseSVG(t, testSVG), SceneConfig{ID: "s01", W: 320, H: 180, Roughness: 1})
	head := sc.items[3]
	if head.anim != "pop" || head.pivot != (geom.Pt{X: 1050, Y: 450}) {
		t.Fatalf("arrowhead: anim %q pivot %v", head.anim, head.pivot)
	}
	if sc.items[2].anim != "draw" || sc.items[4].anim != "draw" {
		t.Fatal("shaft and box must be drawn, not popped")
	}
}

func TestCacheIsTransparent(t *testing.T) {
	sc, plan := planned(t, testSVG, 320, 180, SceneConfig{Roughness: 1, Dim: true}, []int{24000, 24000})
	paper := raster.Paper(320, 180, 1)
	warm := newWorker(paper)
	for f := range int(plan.Scenes[0].Dur * 24) {
		at := float64(f) / 24
		a, b := newImage(warm), newImage(warm)
		sc.frame(warm, at, 0, true, a)
		sc.frame(newWorker(paper), at, 0, true, b)
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Fatalf("frame %d differs between a warm and a cold cache", f)
		}
	}
}

type sink struct{ buf bytes.Buffer }

func (s *sink) Write(b []byte) (int, error) { return s.buf.Write(b) }

func TestParallelEqualsSequential(t *testing.T) {
	sc, plan := planned(t, testSVG, 160, 90, SceneConfig{Roughness: 1}, []int{24000, 24000})
	v := &video{scenes: []*Scene{sc}, plan: plan, q: Quality{W: 160, H: 90, FPS: 24}, marker: true, paper: raster.Paper(160, 90, 1)}
	frames := int(plan.Total() * 24)
	var one, four sink
	for _, run := range []struct {
		jobs int
		out  *sink
	}{{1, &one}, {4, &four}} {
		if err := encodeFrames(t.Context(), run.out, frames, run.jobs, v.paper, v.render, func(int) {}); err != nil {
			t.Fatal(err)
		}
	}
	if one.buf.Len() != frames*160*90*4 || !bytes.Equal(one.buf.Bytes(), four.buf.Bytes()) {
		t.Fatal("parallel output differs from sequential")
	}
}

func TestSwipeSlidesTheSceneOut(t *testing.T) {
	sc1, _ := planned(t, testSVG, 160, 90, SceneConfig{Roughness: 0}, []int{24000, 24000})
	sc2 := PrepareScene(parseSVG(t, testSVG), SceneConfig{ID: "s02", W: 160, H: 90})
	plan := timeline.Build(clipsOf([]int{24000, 24000}, []int{24000, 24000}), 24000, timeline.Default)
	sc1.Plan(plan.Scenes[0])
	sc2.Plan(plan.Scenes[1])
	paper := raster.Paper(160, 90, 1)
	v := &video{scenes: []*Scene{sc1, sc2}, plan: plan, q: Quality{W: 160, H: 90, FPS: 24}, paper: paper}
	ink := func(t0 float64) int {
		img := newImage(newWorker(paper))
		v.render(newWorker(paper), int(t0*24), img)
		n := 0
		for i, b := range img.Pix {
			if b != paper.Pix[i] {
				n++
			}
		}
		return n
	}
	s0 := plan.Scenes[0]
	before := ink(s0.Dur - s0.TransOut - 0.05)
	during := ink(s0.Dur - s0.TransOut/2)
	after := ink(plan.Scenes[1].Start + 0.01)
	if !(during < before && during > 0 && after == 0) {
		t.Fatalf("ink before %d, during %d, after %d", before, during, after)
	}
}

func TestMarkerTravelsWithoutJumps(t *testing.T) {
	sc, plan := planned(t, testSVG, 320, 180, SceneConfig{Roughness: 1}, []int{24000, 24000})
	m := DeviceMat(sc.vb, 320, 180)
	var prev geom.Pt
	seen := false
	for f := range int(plan.Scenes[0].Dur * 240) {
		at := float64(f) / 240
		p, _, _, ok := sc.travel(at, m)
		for _, i := range sc.marks {
			if sp := sc.spans[i]; sp.Start <= at && at < sp.End() {
				ok = false // drawing: the marker is at the stroke tip
			}
		}
		if !ok {
			seen = false
			continue
		}
		if seen && p.Dist(prev) > 60 {
			t.Fatalf("marker jumps %v px at %.3fs", p.Dist(prev), at)
		}
		prev, seen = p, true
	}
}

func TestSpinReturnsAfterFullTurn(t *testing.T) {
	src := `<svg viewBox="0 0 1600 900"><g data-beat="1" data-spin="360"><circle cx="400" cy="400" r="100" fill="#a5d8ff" stroke="#222" stroke-width="5"/></g>
<g data-beat="2"><path d="M100 100 L300 100" fill="none" stroke="#222" stroke-width="5"/></g></svg>`
	sc, _ := planned(t, src, 320, 180, SceneConfig{}, []int{24000, 24000})
	mo := sc.items[0].motion
	if math.IsInf(mo.start, 0) || mo.start <= 0 {
		t.Fatalf("motion start %v", mo.start)
	}
	m := mo.mat(mo.start + 1)
	for i, v := range geom.Identity {
		if math.Abs(m[i]-v) > 1e-9 {
			t.Fatalf("after one turn %v", m)
		}
	}
	if !mo.moving(mo.start+0.5) || mo.moving(mo.start-0.1) {
		t.Fatal("moving() wrong")
	}
}

func TestMoveFollowsGuide(t *testing.T) {
	src := `<svg viewBox="0 0 1600 900"><path id="road" data-role="guide" d="M200 700 L1400 700" fill="none"/>
<g data-beat="1" data-move="#road" data-move-dur="2"><rect x="150" y="650" width="100" height="100" fill="#b2f2bb"/></g>
<g data-beat="2"><path d="M100 100 L300 100" fill="none" stroke="#222" stroke-width="5"/></g></svg>`
	sc, _ := planned(t, src, 320, 180, SceneConfig{}, []int{24000, 24000})
	mo := sc.items[0].motion
	end := mo.mat(mo.start + 5).Apply(geom.Pt{X: 200, Y: 700})
	if end.Dist(geom.Pt{X: 1400, Y: 700}) > 1e-6 {
		t.Fatalf("ends at %v", end)
	}
}

const boardSVG = `<svg viewBox="0 0 3200 1800">
  <g data-beat="1"><circle cx="500" cy="500" r="150" fill="#a5d8ff" stroke="#222" stroke-width="6"/></g>
  <g data-beat="2"><circle cx="2700" cy="1300" r="150" fill="#b2f2bb" stroke="#222" stroke-width="6"/></g>
  <g data-beat="3"><circle cx="2650" cy="1250" r="60" fill="#ffc9c9" stroke="#222" stroke-width="6"/></g>
</svg>`

func TestCameraPlan(t *testing.T) {
	sc, plan := planned(t, boardSVG, 320, 180, SceneConfig{Camera: true, BeatCams: []string{"", "", ""}}, []int{24000, 24000, 24000})
	cam := sc.camera
	if len(cam.moves) != 2 {
		t.Fatalf("want a move to beat 2 and the final zoom-out, got %+v", cam.moves)
	}
	for f := range int(plan.Scenes[0].Dur * 24) {
		v := cam.at(float64(f) / 24)
		if math.Abs(v.W()/v.H()-16.0/9) > 1e-6 || v.W() < minViewW-1e-6 {
			t.Fatalf("view %v", v)
		}
	}
	if got := cam.at(plan.Scenes[0].Dur); got != sc.boardView() {
		t.Fatalf("scene ends on %v, want the whole board", got)
	}
	if mv := cam.moves[0]; sc.beatStart[1] < mv.t1-arriveEarly-1e-9 {
		t.Fatalf("camera arrives at %v, beat 2 draws at %v", mv.t1, sc.beatStart[1])
	}
}

func TestEncoderArgs(t *testing.T) {
	args := strings.Join(encoderArgs(Qualities["low"], "/x/a.wav", "/x/o.mp4"), " ")
	for _, want := range []string{"-map 0:v:0 -map 1:a:0", "-shortest", "+faststart", "-s 854x480", "-pix_fmt yuv420p"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
	silent := strings.Join(encoderArgs(Qualities["low"], "", "/x/o.mp4"), " ")
	if strings.Contains(silent, "-shortest") || strings.Contains(silent, "1:a") {
		t.Errorf("silent args mention audio: %s", silent)
	}
}

func TestVideoSilentEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	board := `{"title":"T","scenes":[{"id":"s01","title":"T","svg":"scenes/s01.svg","beats":[
  {"label":"ЗАГОЛОВОК","narration":"Заголовок."},{"label":"СВЯЗЬ","narration":"Связь."}]}]}`
	for name, body := range map[string]string{project.File: board, "scenes/s01.svg": testSVG} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proj, probs, err := project.Load(dir)
	if err != nil || project.HasErrors(probs) {
		t.Fatalf("load: %v %v", err, probs)
	}
	out := filepath.Join(dir, "out", "v.mp4")
	res, err := Video(t.Context(), proj, out, Options{Quality: Qualities["low"], NoAudio: true, Marker: true})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(res.Path); err != nil || st.Size() == 0 {
		t.Fatalf("no output: %v", err)
	}
	// Only the output file is left behind: the temp dir is gone.
	entries, _ := os.ReadDir(filepath.Dir(out))
	if len(entries) != 1 {
		t.Fatalf("leftovers next to the output: %v", entries)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Video(ctx, proj, filepath.Join(dir, "out", "c.mp4"), Options{Quality: Qualities["low"], NoAudio: true}); err == nil {
		t.Fatal("canceled render succeeded")
	}
	entries, _ = os.ReadDir(filepath.Dir(out))
	if len(entries) != 1 {
		t.Fatalf("canceled render left files: %v", entries)
	}
}

func BenchmarkFrame(b *testing.B) {
	sc, plan := planned(b, testSVG, 1280, 720, SceneConfig{Roughness: 1}, []int{24000, 24000})
	w := newWorker(raster.Paper(1280, 720, 1))
	img := newImage(w)
	at := plan.Scenes[0].Beats[1].ClipStart + 0.3
	for b.Loop() {
		sc.frame(w, at, 0, true, img)
	}
}
