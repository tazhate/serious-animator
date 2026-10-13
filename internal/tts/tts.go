// Package tts turns beat narrations into PCM clips. Each beat is synthesized
// on its own, so beat boundaries are exact instead of guessed from pauses.
package tts

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tazhate/serious-animator/internal/audio"
	"github.com/tazhate/serious-animator/internal/xexec"
)

// Voice selects the speaker and speed.
type Voice struct {
	Name string // e.g. ru-RU-DmitryNeural
	Rate string // e.g. +0%, -10%
}

// Word is a spoken word and when it is said, seconds from the clip start.
type Word struct {
	Text  string  `json:"text"`
	Start float64 `json:"offset"`
	Dur   float64 `json:"duration"`
}

// Clip is one synthesized sentence: mono PCM at audio.Rate plus word timings
// when the engine reports them.
type Clip struct {
	PCM   []int16
	Words []Word
}

// Engine synthesizes one sentence.
type Engine interface {
	Name() string
	Synthesize(ctx context.Context, text string, v Voice) (Clip, error)
}

// WordTimer is an engine that can report word timings.
type WordTimer interface {
	TimesWords() bool
}

// EdgeTTS calls edge-tts: through its Python library when it is importable,
// which also yields word timings, and through the command-line tool otherwise.
type EdgeTTS struct {
	Bin     string        // defaults to "edge-tts"
	Timeout time.Duration // per call, defaults to 60s
	NoWords bool          // skip word timings, use the command-line tool
}

func (e EdgeTTS) Name() string { return "edge-tts" }

//go:embed words.py
var wordsScript []byte

// python finds the interpreter edge-tts runs under, so a pipx or venv install
// works, and checks that it can import the library. It runs once.
var python = sync.OnceValues(func() (string, error) {
	py := "python3"
	if bin, err := exec.LookPath("edge-tts"); err == nil {
		if f, err := os.Open(bin); err == nil {
			first, _ := bufio.NewReader(f).ReadString('\n')
			f.Close()
			if interp, ok := strings.CutPrefix(strings.TrimSpace(first), "#!"); ok {
				fields := strings.Fields(interp)
				if len(fields) > 0 && strings.Contains(filepath.Base(fields[0]), "python") {
					py = fields[0]
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := xexec.Run(ctx, py, "-c", "import edge_tts"); err != nil {
		return "", fmt.Errorf("%s cannot import edge_tts: %w", py, err)
	}
	return py, nil
})

// WordTiming reports whether edge-tts word timings are available, and why not.
func WordTiming() (string, error) { return python() }

func (e EdgeTTS) TimesWords() bool {
	if e.NoWords {
		return false
	}
	_, err := python()
	return err == nil
}

func (e EdgeTTS) Synthesize(ctx context.Context, text string, v Voice) (Clip, error) {
	bin := e.Bin
	if bin == "" {
		bin = "edge-tts"
	}
	timeout := e.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, fmt.Errorf("edge-tts took longer than %s", timeout))
	defer cancel()

	dir, err := os.MkdirTemp("", "serious-animator-tts-*")
	if err != nil {
		return Clip{}, err
	}
	defer os.RemoveAll(dir)
	// The text goes through a file: as an argument, a sentence starting with
	// "-" would be parsed as a flag.
	txt := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(txt, []byte(text), 0o600); err != nil {
		return Clip{}, err
	}
	mp3 := filepath.Join(dir, "out.mp3")
	var words []Word
	if e.TimesWords() {
		py, _ := python()
		script := filepath.Join(dir, "words.py")
		js := filepath.Join(dir, "words.json")
		if err := os.WriteFile(script, wordsScript, 0o600); err != nil {
			return Clip{}, err
		}
		if _, err := xexec.Run(ctx, py, script, txt, v.Name, v.Rate, mp3, js); err != nil {
			return Clip{}, err
		}
		b, err := os.ReadFile(js)
		if err != nil {
			return Clip{}, err
		}
		if err := json.Unmarshal(b, &words); err != nil {
			return Clip{}, fmt.Errorf("word timings: %w", err)
		}
	} else if _, err := xexec.Run(ctx, bin, "--voice", v.Name, "--rate="+v.Rate, "--file", txt, "--write-media", mp3); err != nil {
		return Clip{}, err
	}
	pcm, err := audio.Decode(ctx, mp3)
	return Clip{PCM: pcm, Words: words}, err
}

// Cached stores clips on disk by a hash of engine, voice and text, so
// re-rendering after a drawing fix does not call the TTS again.
type Cached struct {
	Engine Engine
	Dir    string
}

func (c Cached) Name() string { return c.Engine.Name() }

func (c Cached) key(text string, v Voice) string {
	h := sha256.Sum256([]byte(strings.Join([]string{c.Engine.Name(), v.Name, v.Rate, text}, "\x00")))
	return hex.EncodeToString(h[:])
}

func (c Cached) TimesWords() bool {
	wt, ok := c.Engine.(WordTimer)
	return ok && wt.TimesWords()
}

// Synthesize returns the cached clip when there is one. A clip cached
// without word timings is fetched again once the engine can time words;
// if that fails, the cached audio is used as it is.
func (c Cached) Synthesize(ctx context.Context, text string, v Voice) (Clip, error) {
	base := filepath.Join(c.Dir, c.key(text, v))
	var cached []int16
	if b, err := os.ReadFile(base + ".pcm"); err == nil && len(b) > 0 {
		cached = audio.FromBytes(b)
		if js, err := os.ReadFile(base + ".words.json"); err == nil {
			var words []Word
			if json.Unmarshal(js, &words) == nil {
				return Clip{PCM: cached, Words: words}, nil
			}
		}
		if !c.TimesWords() {
			return Clip{PCM: cached}, nil
		}
	}
	clip, err := c.Engine.Synthesize(ctx, text, v)
	if err != nil {
		if cached != nil {
			return Clip{PCM: cached}, nil
		}
		return Clip{}, err
	}
	if err := audio.WriteFileAtomic(base+".pcm", audio.ToBytes(clip.PCM)); err != nil {
		return Clip{}, fmt.Errorf("tts cache: %w", err)
	}
	if clip.Words != nil {
		js, err := json.Marshal(clip.Words)
		if err == nil {
			err = audio.WriteFileAtomic(base+".words.json", js)
		}
		if err != nil {
			return Clip{}, fmt.Errorf("tts cache: %w", err)
		}
	}
	return clip, nil
}

// DefaultCacheDir is $XDG_CACHE_HOME/serious-animator/tts.
func DefaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "serious-animator", "tts")
}

// Options control SynthesizeAll.
type Options struct {
	Parallel int           // concurrent requests, defaults to 2 (edge-tts answers 403 to more)
	Attempts int           // per clip, defaults to 5: edge-tts answers 403 in bursts
	Backoff  time.Duration // first retry delay, doubled each time; defaults to 2s
	Progress func(done, total int)
}

// SynthesizeAll synthesizes every text, a limited number at a time, with
// retries on failure. A clip that fails does not cancel the others: with a
// Cached engine every success is kept, so running again only fetches what is
// missing. Clips are trimmed of edge silence, and word timings move with
// the trim. Progress may be called from several goroutines.
func SynthesizeAll(ctx context.Context, eng Engine, texts []string, v Voice, o Options) ([]Clip, error) {
	if o.Parallel <= 0 {
		o.Parallel = 2
	}
	if o.Attempts <= 0 {
		o.Attempts = 5
	}
	if o.Backoff <= 0 {
		o.Backoff = 2 * time.Second
	}
	out := make([]Clip, len(texts))
	errs := make([]error, len(texts))
	var done atomic.Int64
	var g errgroup.Group
	g.SetLimit(o.Parallel)
	for i, text := range texts {
		g.Go(func() error {
			clip, err := withRetry(ctx, o, func() (Clip, error) { return eng.Synthesize(ctx, text, v) })
			if err == nil {
				var lead int
				clip.PCM, lead = audio.TrimSilenceOffset(clip.PCM, audio.Rate/25)
				if len(clip.PCM) == 0 {
					err = fmt.Errorf("%s returned silence", eng.Name())
				}
				shift := float64(lead) / audio.Rate
				for k := range clip.Words {
					clip.Words[k].Start = max(clip.Words[k].Start-shift, 0)
				}
			}
			if err != nil {
				errs[i] = fmt.Errorf("narration %d %q: %w", i+1, short(text), err)
				return nil
			}
			out[i] = clip
			n := done.Add(1)
			if o.Progress != nil {
				o.Progress(int(n), len(texts))
			}
			return nil
		})
	}
	_ = g.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("%d of %d clips failed. Finished clips are cached, so running again only retries the rest. "+
			"If the same clips keep failing with NoAudioReceived, the voice %s is unavailable right now: "+
			"pick another one with `serious-animator voices --lang xx`.\n%w", len(texts)-int(done.Load()), len(texts), v.Name, err)
	}
	return out, nil
}

// maxBackoff caps a single retry wait.
const maxBackoff = 15 * time.Second

func withRetry(ctx context.Context, o Options, f func() (Clip, error)) (Clip, error) {
	var last error
	delay := o.Backoff
	for attempt := range o.Attempts {
		clip, err := f()
		if err == nil {
			return clip, nil
		}
		last = err
		if ctx.Err() != nil {
			return Clip{}, err
		}
		if attempt == o.Attempts-1 {
			break
		}
		// Jitter keeps parallel workers from retrying in lockstep.
		wait := delay + rand.N(delay/2)
		select {
		case <-ctx.Done():
			return Clip{}, context.Cause(ctx)
		case <-time.After(wait):
		}
		delay = min(delay*2, maxBackoff)
	}
	return Clip{}, fmt.Errorf("failed %d times, last error: %w", o.Attempts, last)
}

func short(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
