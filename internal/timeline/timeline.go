// Package timeline lays narration clips out on the video's time axis and
// schedules every drawn element inside the window of its beat.
//
// Positions are computed in audio samples first and converted to seconds
// afterwards, so the audio track and the frames cannot drift apart.
package timeline

// Layout holds the silences around and between narration clips.
type Layout struct {
	Lead      float64 // before the first beat of each scene
	Gap       float64 // between beats
	Tail      float64 // after the last beat of a scene: hold + transition
	FinalTail float64 // after the last beat of the video
	Trans     float64 // transition into the next scene, the end of Tail
}

// Default matches the pacing of a calm explainer.
var Default = Layout{Lead: 0.4, Gap: 0.35, Tail: 1.3, FinalTail: 1.5, Trans: 0.6}

// anchorLead starts drawing a little before the label word is spoken, so the
// pen is moving when the viewer hears the word.
const anchorLead = 0.3

// Clip is one narration clip waiting to be placed.
type Clip struct {
	Samples int
	Pause   float64 // extra silence after the clip, seconds
	// Anchor is where in the clip the beat's label word starts, seconds;
	// negative when unknown. Drawing then starts at the clip start.
	Anchor float64
}

// SceneClips is the input for one scene.
type SceneClips struct {
	Clips     []Clip
	ExtraTail float64 // more hold at the end, e.g. for a camera zoom-out
}

// Beat is one narration clip. Times are relative to the scene start.
type Beat struct {
	ClipStart, ClipEnd float64
	// DrawStart is when drawing should start: at the label word if known.
	DrawStart float64
	// DrawEnd bounds the drawing window: up to the next clip, or the clip's
	// end for the last beat.
	DrawEnd     float64
	StartSample int // global position of the clip in the audio track
}

// Scene is one SVG drawing on the time axis.
type Scene struct {
	Start, Dur  float64 // global seconds
	StartSample int
	Samples     int
	Beats       []Beat
	TransOut    float64 // seconds of transition at the end, 0 for the last scene
}

// Plan is the whole video.
type Plan struct {
	Rate         int
	Scenes       []Scene
	TotalSamples int
}

// Total returns the video length in seconds.
func (p Plan) Total() float64 { return float64(p.TotalSamples) / float64(p.Rate) }

// Build places clips on the axis, scene after scene.
func Build(scenes []SceneClips, rate int, l Layout) Plan {
	sec := func(n int) float64 { return float64(n) / float64(rate) }
	smp := func(s float64) int { return int(s*float64(rate) + 0.5) }
	lead, gap := smp(l.Lead), smp(l.Gap)
	p := Plan{Rate: rate}
	pos := 0
	for si, in := range scenes {
		sc := Scene{StartSample: pos, Start: sec(pos)}
		local := lead
		for bi, c := range in.Clips {
			b := Beat{ClipStart: sec(local), ClipEnd: sec(local + c.Samples), StartSample: pos + local}
			b.DrawStart = b.ClipStart
			if c.Anchor >= 0 {
				b.DrawStart = b.ClipStart + max(min(c.Anchor, sec(c.Samples))-anchorLead, 0)
			}
			local += c.Samples + smp(c.Pause)
			if bi < len(in.Clips)-1 {
				local += gap
			}
			b.DrawEnd = sec(local)
			sc.Beats = append(sc.Beats, b)
		}
		tail := l.Tail
		if si == len(scenes)-1 {
			tail = l.FinalTail
		} else {
			sc.TransOut = l.Trans
		}
		local += smp(tail + in.ExtraTail)
		sc.Samples = local
		sc.Dur = sec(local)
		p.Scenes = append(p.Scenes, sc)
		pos += local
	}
	p.TotalSamples = pos
	return p
}

// Req is an element waiting to be scheduled.
type Req struct {
	Beat    int     // 1-based
	Natural float64 // preferred drawing time, seconds
}

// Span is when an element is drawn, relative to the scene start.
type Span struct{ Start, Dur float64 }

// End returns the time the element is fully drawn.
func (s Span) End() float64 { return s.Start + s.Dur }

const (
	itemGap    = 0.08 // pause between two elements of one beat
	budgetFrac = 0.9  // share of the beat window available for drawing
	fillFrac   = 0.7  // short beats stretch their drawing up to this share
	maxStretch = 1.6  // but never slow an element down more than this
)

// Schedule assigns each element a span inside its beat's window. Elements of
// one beat are drawn one after another in document order, starting at the
// beat's DrawStart; when that leaves too little time, drawing starts earlier,
// at the clip start at the latest. A crowded beat is compressed to fit, a
// sparse one is slowed down a little; either way the beat never spills past
// its window. Elements with an out-of-range beat get a zero-length span at
// the scene start (validation rejects such scenes, so this only keeps
// rendering total).
func Schedule(beats []Beat, reqs []Req) []Span {
	spans := make([]Span, len(reqs))
	byBeat := make([][]int, len(beats))
	for i, r := range reqs {
		if r.Beat >= 1 && r.Beat <= len(beats) {
			byBeat[r.Beat-1] = append(byBeat[r.Beat-1], i)
		}
	}
	for bi, idx := range byBeat {
		if len(idx) == 0 {
			continue
		}
		b := beats[bi]
		need := itemGap * float64(len(idx)-1)
		for _, i := range idx {
			need += max(reqs[i].Natural, 0)
		}
		start := min(max(b.DrawStart, b.ClipStart), b.DrawEnd)
		if need > (b.DrawEnd-start)*budgetFrac {
			start = max(b.ClipStart, b.DrawEnd-need/budgetFrac)
		}
		budget := max(b.DrawEnd-start, 0) * budgetFrac
		k := 1.0
		switch {
		case need <= 0:
			k = 0
		case need > budget:
			k = budget / need
		case need < fillFrac*budget:
			k = min(maxStretch, fillFrac*budget/need)
		}
		t := start
		for j, i := range idx {
			d := max(reqs[i].Natural, 0) * k
			spans[i] = Span{Start: t, Dur: d}
			t += d
			if j < len(idx)-1 {
				t += itemGap * k
			}
		}
	}
	return spans
}
