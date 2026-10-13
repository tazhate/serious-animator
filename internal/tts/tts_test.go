package tts

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake fails each text a configured number of times before answering.
type fake struct {
	mu    sync.Mutex
	fails map[string]int
	calls map[string]int
}

func (f *fake) Name() string { return "fake" }

func (f *fake) Synthesize(_ context.Context, text string, _ Voice) (Clip, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[text]++
	if f.fails[text] > 0 {
		f.fails[text]--
		return Clip{}, errors.New("403")
	}
	pcm := make([]int16, 5000)
	pcm[2000], pcm[2001] = 9000, 9000
	return Clip{PCM: pcm, Words: []Word{{Text: text, Start: 0.1, Dur: 0.05}}}, nil
}

func TestSynthesizeAllRetriesAndKeepsGoing(t *testing.T) {
	f := &fake{fails: map[string]int{"a": 1, "b": 99}, calls: map[string]int{}}
	cache := Cached{Engine: f, Dir: t.TempDir()}
	o := Options{Attempts: 2, Backoff: time.Millisecond}
	_, err := SynthesizeAll(t.Context(), cache, []string{"a", "b", "c"}, Voice{Name: "v", Rate: "+0%"}, o)
	if err == nil || !strings.Contains(err.Error(), "1 of 3 clips failed") {
		t.Fatalf("err = %v", err)
	}
	if f.calls["a"] != 2 || f.calls["c"] != 1 || f.calls["b"] != 2 {
		t.Fatalf("calls %v", f.calls)
	}
	// Second run: a and c come from the cache, only b is asked again.
	f.fails["b"] = 0
	out, err := SynthesizeAll(t.Context(), cache, []string{"a", "b", "c"}, Voice{Name: "v", Rate: "+0%"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls["a"] != 2 || f.calls["c"] != 1 || f.calls["b"] != 3 {
		t.Fatalf("cache not used, calls %v", f.calls)
	}
	if len(out[0].PCM) != 2+2*960 {
		t.Fatalf("clip not trimmed: %v", out[0].PCM)
	}
	// The trim cut 2000-960 samples from the start: the word moves with it.
	if w := out[0].Words[0].Start; math.Abs(w-(0.1-1040.0/24000)) > 1e-9 {
		t.Fatalf("word start %v not shifted by the trim", w)
	}
}
