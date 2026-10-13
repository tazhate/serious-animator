package timeline

import (
	"math"
	"testing"
)

func TestBuildIsSampleExact(t *testing.T) {
	const rate = 24000
	p := Build(clipsOf([][]int{{24000, 36000}, {12000}}), rate, Default)
	if len(p.Scenes) != 2 {
		t.Fatalf("scenes: %d", len(p.Scenes))
	}
	sum := 0
	for _, s := range p.Scenes {
		sum += s.Samples
	}
	if sum != p.TotalSamples {
		t.Fatalf("scene samples %d != total %d", sum, p.TotalSamples)
	}
	s0 := p.Scenes[0]
	// lead 0.4 + 1.0 + gap 0.35 + 1.5 + tail 1.3
	if math.Abs(s0.Dur-4.55) > 1e-9 {
		t.Errorf("scene 0 dur %v", s0.Dur)
	}
	if b := s0.Beats[1]; math.Abs(b.ClipStart-1.75) > 1e-9 || math.Abs(b.ClipEnd-3.25) > 1e-9 || b.DrawEnd != b.ClipEnd {
		t.Errorf("beat 2: %+v", b)
	}
	if b := s0.Beats[0]; b.DrawEnd != s0.Beats[1].ClipStart {
		t.Errorf("beat 1 draw window should reach the next clip: %+v", b)
	}
	if s0.TransOut == 0 || p.Scenes[1].TransOut != 0 {
		t.Error("only non-final scenes have a transition")
	}
	if b := s0.Beats[0]; b.DrawStart != b.ClipStart {
		t.Errorf("unanchored beat starts drawing at %v, clip at %v", b.DrawStart, b.ClipStart)
	}
	if p.Scenes[1].Beats[0].StartSample != s0.Samples+int(0.4*rate) {
		t.Errorf("global sample position %d", p.Scenes[1].Beats[0].StartSample)
	}
}

func TestScheduleCrowdedBeatStaysInWindow(t *testing.T) {
	beats := []Beat{{ClipStart: 0.4, DrawStart: 0.4, ClipEnd: 1.4, DrawEnd: 1.4}}
	var reqs []Req
	for range 40 {
		reqs = append(reqs, Req{Beat: 1, Natural: 1.5})
	}
	spans := Schedule(beats, reqs)
	last := spans[len(spans)-1]
	if last.End() > beats[0].DrawEnd+1e-9 {
		t.Fatalf("crowded beat ends at %v, window ends at %v", last.End(), beats[0].DrawEnd)
	}
	for i := 1; i < len(spans); i++ {
		if spans[i].Start < spans[i-1].End()-1e-9 {
			t.Fatalf("spans overlap at %d", i)
		}
	}
}

func TestScheduleSparseBeatStretchesWithinLimit(t *testing.T) {
	beats := []Beat{{ClipStart: 0, ClipEnd: 10, DrawEnd: 10}}
	spans := Schedule(beats, []Req{{Beat: 1, Natural: 1}})
	if spans[0].Dur != maxStretch {
		t.Fatalf("dur %v, want %v", spans[0].Dur, maxStretch)
	}
}

func TestScheduleOrderAcrossBeats(t *testing.T) {
	beats := []Beat{{ClipStart: 0, DrawEnd: 2}, {ClipStart: 2, DrawStart: 2, DrawEnd: 4}}
	spans := Schedule(beats, []Req{{Beat: 2, Natural: 0.5}, {Beat: 1, Natural: 0.5}, {Beat: 9, Natural: 1}})
	if spans[0].Start != 2 || spans[1].Start != 0 {
		t.Fatalf("spans %+v", spans)
	}
	if spans[2] != (Span{}) {
		t.Fatalf("out-of-range beat got %+v", spans[2])
	}
}

func clipsOf(n [][]int) []SceneClips {
	out := make([]SceneClips, len(n))
	for i, sc := range n {
		for _, c := range sc {
			out[i].Clips = append(out[i].Clips, Clip{Samples: c, Anchor: -1})
		}
	}
	return out
}

func TestAnchorAndPause(t *testing.T) {
	const rate = 1000
	p := Build([]SceneClips{{Clips: []Clip{
		{Samples: 2000, Anchor: 1.5, Pause: 0.8},
		{Samples: 1000, Anchor: -1},
	}}}, rate, Default)
	b0, b1 := p.Scenes[0].Beats[0], p.Scenes[0].Beats[1]
	if math.Abs(b0.DrawStart-(b0.ClipStart+1.2)) > 1e-9 {
		t.Fatalf("anchored draw start %v, clip %v", b0.DrawStart, b0.ClipStart)
	}
	if math.Abs(b1.ClipStart-(b0.ClipEnd+0.8+Default.Gap)) > 1e-9 {
		t.Fatalf("pause not added: %v -> %v", b0.ClipEnd, b1.ClipStart)
	}
	// Enough room after the anchor: the element starts at the anchor.
	sp := Schedule(p.Scenes[0].Beats, []Req{{Beat: 1, Natural: 0.5}})
	if math.Abs(sp[0].Start-b0.DrawStart) > 1e-9 {
		t.Fatalf("starts at %v, want %v", sp[0].Start, b0.DrawStart)
	}
	// Too much to draw after the anchor: drawing moves earlier, not faster.
	sp = Schedule(p.Scenes[0].Beats, []Req{{Beat: 1, Natural: 2.5}})
	if sp[0].Start >= b0.DrawStart || sp[0].Start < b0.ClipStart || sp[0].End() > b0.DrawEnd+1e-9 {
		t.Fatalf("crowded anchored beat: %+v, beat %+v", sp[0], b0)
	}
}
