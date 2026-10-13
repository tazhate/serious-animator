package sfx

import (
	"math"
	"testing"
)

func peak(x []float64) (int, float64) {
	at, p := -1, 0.0
	for i, v := range x {
		if math.Abs(v) > p {
			at, p = i, math.Abs(v)
		}
	}
	return at, p
}

func TestEffectsStayQuietAndInPlace(t *testing.T) {
	const rate = 24000
	tr := Track([]Event{{T: 1, Kind: Pop}, {T: 2, Kind: Whoosh, Dur: 0.8}, {T: 3.5, Kind: Scratch, Dur: 1}}, 5*rate, rate)
	at, p := peak(tr)
	if p > math.Pow(10, -1.0/20) {
		t.Fatalf("peak %v above -1 dBFS", p)
	}
	if at < rate || at > rate+rate/10 {
		t.Fatalf("loudest sample at %d, want the pop right after 1 s", at)
	}
	for i := range rate {
		if tr[i] != 0 {
			t.Fatal("sound before the first event")
		}
	}
}

func TestPadFadesInAndOut(t *testing.T) {
	const rate = 8000
	p := Pad(10*rate, rate)
	if p[0] != 0 || p[len(p)-1] != 0 {
		t.Fatal("pad does not fade at the edges")
	}
	if _, pk := peak(p); pk < 0.05 || pk > 1 {
		t.Fatalf("pad peak %v", pk)
	}
}
