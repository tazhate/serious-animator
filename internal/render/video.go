package render

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tazhate/serious-animator/internal/audio"
	"github.com/tazhate/serious-animator/internal/ease"
	"github.com/tazhate/serious-animator/internal/project"
	"github.com/tazhate/serious-animator/internal/raster"
	"github.com/tazhate/serious-animator/internal/sfx"
	"github.com/tazhate/serious-animator/internal/textmatch"
	"github.com/tazhate/serious-animator/internal/timeline"
	"github.com/tazhate/serious-animator/internal/tts"
	"github.com/tazhate/serious-animator/internal/xexec"
)

// Options control Video.
type Options struct {
	Quality   Quality
	Roughness float64
	Marker    bool
	NoAudio   bool
	NoMusic   bool
	NoSFX     bool
	NoWords   bool       // draw at the clip start, not at the label word
	Jobs      int        // frame workers, defaults to the CPU count (at most 8)
	TTS       tts.Engine // required unless NoAudio
	Log       *slog.Logger
}

// silentRate is the reading speed assumed for --no-audio, runes per second.
const silentRate = 14.0

// Result describes a rendered video.
type Result struct {
	Path     string
	Duration time.Duration
	Frames   int
	Scenes   int
}

// Configs returns the per-scene settings from the storyboard.
// q must already be Oriented to the storyboard's format.
func Configs(proj *project.Project, q Quality, roughness float64) []SceneConfig {
	fw, fh := proj.Board.Frame()
	out := make([]SceneConfig, len(proj.Board.Scenes))
	for si, sc := range proj.Board.Scenes {
		cams := make([]string, len(sc.Beats))
		for bi, b := range sc.Beats {
			cams[bi] = b.Camera
		}
		on := false
		if d := proj.Drawings[si]; d != nil {
			on = project.CameraOn(sc, d.ViewBox, fw, fh)
		}
		out[si] = SceneConfig{
			ID: sc.ID, W: q.W, H: q.H, FrameW: fw, FrameH: fh, Roughness: roughness,
			Camera: on, Dim: sc.Dim, Transition: sc.Transition, BeatCams: cams,
		}
	}
	return out
}

// Video renders a validated project into an MP4 at out.
func Video(ctx context.Context, proj *project.Project, out string, o Options) (Result, error) {
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	board := proj.Board
	q := o.Quality.Oriented(board.Vertical())

	// 1. Narration clips, one per beat.
	var texts []string
	for _, sc := range board.Scenes {
		for _, b := range sc.Beats {
			texts = append(texts, b.Narration)
		}
	}
	var clips []tts.Clip
	if o.NoAudio {
		for _, t := range texts {
			n := int(max(1.2, float64(utf8.RuneCountInString(t))/silentRate) * audio.Rate)
			clips = append(clips, tts.Clip{PCM: make([]int16, n)})
		}
	} else {
		if o.TTS == nil {
			return Result{}, errors.New("no TTS engine configured")
		}
		log.Info("synthesizing narration", "clips", len(texts), "engine", o.TTS.Name(), "voice", board.Voice)
		var err error
		clips, err = tts.SynthesizeAll(ctx, o.TTS, texts, tts.Voice{Name: board.Voice, Rate: board.Rate}, tts.Options{
			Progress: func(done, total int) { log.Debug("tts", "done", done, "total", total) },
		})
		if err != nil {
			return Result{}, err
		}
	}

	// 2. Prepare scenes, then lay the clips out on the time axis.
	cfgs := Configs(proj, q, o.Roughness)
	scenes := make([]*Scene, len(board.Scenes))
	in := make([]timeline.SceneClips, len(board.Scenes))
	k := 0
	anchored := 0
	for si, sc := range board.Scenes {
		d := proj.Drawings[si]
		if d == nil {
			return Result{}, fmt.Errorf("scene %s has no drawing", sc.ID)
		}
		scenes[si] = PrepareScene(d, cfgs[si])
		if cfgs[si].Camera {
			in[si].ExtraTail = zoomOutTail
		}
		for _, b := range sc.Beats {
			c := timeline.Clip{Samples: len(clips[k].PCM), Pause: b.Pause, Anchor: -1}
			if !o.NoWords {
				if a, ok := labelAnchor(b.Label, clips[k].Words); ok {
					c.Anchor = a
					anchored++
				}
			}
			in[si].Clips = append(in[si].Clips, c)
			k++
		}
	}
	plan := timeline.Build(in, audio.Rate, timeline.Default)
	for si := range scenes {
		scenes[si].Plan(plan.Scenes[si])
	}
	if !o.NoAudio {
		log.Info("timing", "beats", len(texts), "synced to label word", anchored)
	}

	// 3. Work in a temp dir next to the output so the final rename is atomic.
	absOut, err := filepath.Abs(out)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return Result{}, err
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(absOut), ".serious-animator-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmpDir)

	var wav string
	if !o.NoAudio {
		wav, err = soundtrack(ctx, proj, plan, scenes, clips, tmpDir, o, log)
		if err != nil {
			return Result{}, err
		}
	}

	// 4. Encode.
	total := plan.Total()
	frames := int(math.Ceil(total * float64(q.FPS)))
	tmpOut := filepath.Join(tmpDir, "video.mp4")
	enc, err := xexec.Start(ctx, "ffmpeg", encoderArgs(q, wav, tmpOut)...)
	if err != nil {
		return Result{}, err
	}
	jobs := o.Jobs
	if jobs <= 0 {
		jobs = min(runtime.GOMAXPROCS(0), 8)
	}
	log.Info("rendering", "frames", frames, "duration", fmt.Sprintf("%.1fs", total), "size", fmt.Sprintf("%dx%d@%d", q.W, q.H, q.FPS), "jobs", jobs)
	started := time.Now()
	paper := raster.Paper(q.W, q.H, 1)
	v := &video{scenes: scenes, plan: plan, q: q, marker: o.Marker, paper: paper}
	lastPct := -1
	err = encodeFrames(ctx, enc, frames, jobs, paper, v.render, func(done int) {
		if pct := 100 * done / frames; pct/10 != lastPct/10 {
			lastPct = pct
			log.Info("progress", "percent", pct)
		}
	})
	if err != nil {
		enc.Kill()
		return Result{}, err
	}
	if err := enc.Wait(); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpOut, absOut); err != nil {
		return Result{}, err
	}
	log.Info("done", "file", absOut, "took", time.Since(started).Round(time.Millisecond))
	return Result{
		Path:     absOut,
		Duration: time.Duration(total * float64(time.Second)),
		Frames:   frames,
		Scenes:   len(board.Scenes),
	}, nil
}

// labelAnchor finds when the beat's label is first spoken.
func labelAnchor(label string, words []tts.Word) (float64, bool) {
	spoken := make([]string, len(words))
	for i, w := range words {
		spoken[i] = w.Text
	}
	if i := textmatch.FirstLabelWord(label, spoken); i >= 0 {
		return words[i].Start, true
	}
	return 0, false
}

// video maps a frame number to the scene that shows it.
type video struct {
	scenes []*Scene
	plan   timeline.Plan
	q      Quality
	marker bool
	paper  *image.RGBA
}

func (v *video) render(w *worker, f int, dst *image.RGBA) {
	t := float64(f) / float64(v.q.FPS)
	si := 0
	for si+1 < len(v.plan.Scenes) && t >= v.plan.Scenes[si+1].Start {
		si++
	}
	ps := v.plan.Scenes[si]
	sc := v.scenes[si]
	local := t - ps.Start
	shift, fade := 0.0, 0.0
	if ps.TransOut > 0 && local > ps.Dur-ps.TransOut {
		k := (local - (ps.Dur - ps.TransOut)) / ps.TransOut
		switch sc.trans {
		case project.TransitionSwipe:
			shift = -float64(v.q.W) * 1.05 * ease.InOutCubic(k)
		case project.TransitionFade:
			fade = ease.Smoothstep(k)
		}
	}
	sc.frame(w, local, shift, v.marker, dst)
	if fade > 0 {
		raster.FadeTo(dst, v.paper, fade)
	}
}

// encodeFrames renders frames on jobs workers and writes them to enc in
// order. Each worker takes a run of consecutive frames, so its still-layer
// cache stays warm; a bounded pool of buffers keeps memory flat.
func encodeFrames(ctx context.Context, enc interface{ Write([]byte) (int, error) }, frames, jobs int,
	paper *image.RGBA, render func(w *worker, f int, dst *image.RGBA), progress func(done int)) error {
	frameBytes := len(paper.Pix)
	chunk := max(2, (24<<20)/frameBytes)
	nChunks := (frames + chunk - 1) / chunk
	sets := make(chan []*image.RGBA, jobs+2)
	for range jobs + 2 {
		bufs := make([]*image.RGBA, chunk)
		for i := range bufs {
			bufs[i] = image.NewRGBA(paper.Rect)
		}
		sets <- bufs
	}
	type result struct {
		idx  int
		bufs []*image.RGBA
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	work := make(chan int)
	results := make(chan result, jobs+2)
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(work)
		for c := range nChunks {
			select {
			case work <- c:
			case <-ctx.Done():
				return
			}
		}
	})
	for range jobs {
		wg.Go(func() {
			w := newWorker(paper)
			for {
				// Take a buffer set before a job: the chunk the writer waits
				// for always has its buffers, so the pool cannot starve it.
				var bufs []*image.RGBA
				select {
				case bufs = <-sets:
				case <-ctx.Done():
					return
				}
				c, ok := <-work
				if !ok {
					return
				}
				for i := range chunk {
					if f := c*chunk + i; f < frames {
						render(w, f, bufs[i])
					}
				}
				select {
				case results <- result{c, bufs}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	pending := map[int][]*image.RGBA{}
	var werr error
	for next := 0; next < nChunks && werr == nil; {
		select {
		case r := <-results:
			pending[r.idx] = r.bufs
		case <-ctx.Done():
			werr = context.Cause(ctx)
		}
		for bufs, ok := pending[next]; ok && werr == nil; bufs, ok = pending[next] {
			for i := range chunk {
				f := next*chunk + i
				if f >= frames {
					break
				}
				if _, err := enc.Write(bufs[i].Pix); err != nil {
					werr = fmt.Errorf("write frame: %w", err)
					break
				}
			}
			delete(pending, next)
			sets <- bufs
			next++
			progress(min(next*chunk, frames))
		}
	}
	cancel(werr)
	wg.Wait()
	return werr
}

// soundtrack writes the final audio: narration placed on the time axis,
// effects and music mixed under it and normalized.
func soundtrack(ctx context.Context, proj *project.Project, plan timeline.Plan, scenes []*Scene, clips []tts.Clip, dir string, o Options, log *slog.Logger) (string, error) {
	board := &proj.Board
	track := make([]int16, plan.TotalSamples)
	k := 0
	for si := range board.Scenes {
		for _, b := range plan.Scenes[si].Beats {
			copy(track[b.StartSample:], clips[k].PCM)
			k++
		}
	}
	voice := filepath.Join(dir, "narration.wav")
	if err := audio.WriteWAV(voice, track, audio.Rate); err != nil {
		return "", err
	}
	mix := audio.Mix{Voice: voice, Duration: plan.Total(), MusicDB: board.MusicDB()}
	if board.SFXOn() && !o.NoSFX {
		fx := sfx.ToPCM(sfx.Track(soundEvents(plan, scenes), plan.TotalSamples, audio.Rate))
		mix.SFX = filepath.Join(dir, "sfx.wav")
		if err := audio.WriteWAV(mix.SFX, fx, audio.Rate); err != nil {
			return "", err
		}
	}
	switch {
	case o.NoMusic || board.Music == "":
	case board.Music == project.MusicGenerated:
		pad := sfx.ToPCM(sfx.Pad(plan.TotalSamples, audio.Rate))
		mix.Music = filepath.Join(dir, "music.wav")
		if err := audio.WriteWAV(mix.Music, pad, audio.Rate); err != nil {
			return "", err
		}
	default:
		mix.Music, mix.LoopMusic = filepath.Join(proj.Dir, board.Music), true
	}
	log.Info("mixing soundtrack", "sfx", mix.SFX != "", "music", board.Music != "" && !o.NoMusic)
	out := filepath.Join(dir, "mix.wav")
	if err := mix.Render(ctx, out); err != nil {
		return "", err
	}
	return out, nil
}

// soundEvents lists the effects: pops for elements that pop in, whooshes for
// camera moves and swipes, and a faint scratch while strokes are drawn.
func soundEvents(plan timeline.Plan, scenes []*Scene) []sfx.Event {
	var ev []sfx.Event
	for si, sc := range scenes {
		start := plan.Scenes[si].Start
		var scratchEnd float64
		for i, it := range sc.items {
			sp := sc.spans[i]
			switch {
			case it.popSFX:
				ev = append(ev, sfx.Event{T: start + sp.Start - 0.02, Kind: sfx.Pop})
			case it.anim == "draw" && sp.Dur >= 0.25 && sp.Start >= scratchEnd:
				ev = append(ev, sfx.Event{T: start + sp.Start, Kind: sfx.Scratch, Dur: sp.Dur})
				scratchEnd = sp.End()
			}
		}
		for _, mv := range sc.camera.moves {
			ev = append(ev, sfx.Event{T: start + mv.t0, Kind: sfx.Whoosh, Dur: mv.t1 - mv.t0})
		}
		if ps := plan.Scenes[si]; ps.TransOut > 0 && sc.trans == project.TransitionSwipe {
			ev = append(ev, sfx.Event{T: start + ps.Dur - ps.TransOut - 0.05, Kind: sfx.Whoosh, Dur: ps.TransOut + 0.1})
		}
	}
	return ev
}

// encoderArgs builds one ffmpeg call that encodes the raw frames and muxes
// the whole soundtrack: no per-scene files, no concat, no drift.
func encoderArgs(q Quality, wav, out string) []string {
	args := []string{
		"-nostdin", "-y", "-v", "error",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", q.W, q.H),
		"-r", strconv.Itoa(q.FPS), "-i", "-",
	}
	if wav != "" {
		args = append(args, "-i", wav, "-map", "0:v:0", "-map", "1:a:0")
	} else {
		args = append(args, "-map", "0:v:0")
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(q.CRF),
		"-pix_fmt", "yuv420p",
	)
	if wav != "" {
		args = append(args, "-c:a", "aac", "-b:a", "160k", "-shortest")
	}
	return append(args, "-movflags", "+faststart", out)
}
