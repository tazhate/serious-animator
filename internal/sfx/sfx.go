// Package sfx synthesizes the sound layer: short pops and whooshes that mark
// what happens on screen, a quiet marker scratch, and a soft generated music
// pad. Everything is generated, so there are no sample files to license.
package sfx

import (
	"math"

	"github.com/tazhate/serious-animator/internal/geom"
)

// Kind is a sound effect.
type Kind int

const (
	Pop     Kind = iota // an element pops in
	Whoosh              // the camera moves or the scene changes
	Scratch             // the marker draws; Dur is how long
)

// Event is one sound at time T, seconds from the video start.
type Event struct {
	T    float64
	Kind Kind
	Dur  float64 // Whoosh and Scratch only
}

// Levels relative to full scale. The narration peaks near 0 dBFS, so these
// sit well under the voice.
const (
	popGain     = 0.18 // about -15 dB
	whooshGain  = 0.08 // about -22 dB
	scratchGain = 0.025
)

// Track renders events into a mono track of n samples at rate.
func Track(events []Event, n, rate int) []float64 {
	out := make([]float64, n)
	for i, e := range events {
		at := int(e.T * float64(rate))
		var snd []float64
		seed := geom.Hash64(uint64(i) + 1)
		switch e.Kind {
		case Pop:
			snd = pop(rate, seed)
		case Whoosh:
			snd = whoosh(rate, max(e.Dur, 0.3), seed)
		case Scratch:
			snd = scratch(rate, e.Dur, seed)
		}
		for k, v := range snd {
			if j := at + k; j >= 0 && j < n {
				out[j] += v
			}
		}
	}
	return out
}

// pop is a falling sine blip with a soft click: 70 ms.
func pop(rate int, seed uint64) []float64 {
	n := int(0.07 * float64(rate))
	out := make([]float64, n)
	phase := 0.0
	f0 := 820 + 120*(unit(seed)-0.5)
	for i := range out {
		t := float64(i) / float64(rate)
		f := f0 * math.Exp(-t*16) // sweeps down to about a third
		phase += 2 * math.Pi * f / float64(rate)
		env := math.Exp(-t*45) * min(t/0.003, 1)
		out[i] = popGain * env * math.Sin(phase)
	}
	return out
}

// whoosh is noise through a resonant band-pass whose center sweeps up and
// back down, under a rise-and-fall envelope.
func whoosh(rate int, dur float64, seed uint64) []float64 {
	n := int(dur * float64(rate))
	out := make([]float64, n)
	var lp, bp float64
	rng := seed
	for i := range out {
		u := float64(i) / float64(n)
		rng = geom.Hash64(rng)
		x := unit(rng)*2 - 1
		fc := 300 + 1500*math.Sin(math.Pi*u)
		// Chamberlin state-variable filter.
		f := 2 * math.Sin(math.Pi*fc/float64(rate))
		const q = 0.7
		lp += f * bp
		hp := x - lp - q*bp
		bp += f * hp
		env := math.Pow(math.Sin(math.Pi*u), 1.5)
		out[i] = whooshGain * env * bp
	}
	return out
}

// scratch is a faint, grainy rustle of filtered noise for a drawing stroke.
func scratch(rate int, dur float64, seed uint64) []float64 {
	n := int(dur * float64(rate))
	out := make([]float64, n)
	var lp, prev float64
	rng := seed
	for i := range out {
		t := float64(i) / float64(rate)
		rng = geom.Hash64(rng)
		x := unit(rng)*2 - 1
		hp := x - prev // emphasize the hiss
		prev = x
		lp += 0.35 * (hp - lp)
		grain := 0.6 + 0.4*math.Sin(2*math.Pi*9*t+unit(seed)*6)
		edge := min(t/0.04, (dur-t)/0.06, 1)
		out[i] = scratchGain * max(edge, 0) * grain * lp
	}
	return out
}

// Pad is a calm generated music bed of n samples: four slow chords
// (C - G - Am - F) of soft detuned triangle tones with a gentle filter.
func Pad(n, rate int) []float64 {
	chords := [][]float64{
		{130.81, 196.00, 261.63, 329.63}, // C
		{98.00, 196.00, 246.94, 293.66},  // G
		{110.00, 220.00, 261.63, 329.63}, // Am
		{87.31, 174.61, 220.00, 261.63},  // F
	}
	const chordDur = 4.0
	out := make([]float64, n)
	var lp float64
	phases := make([]float64, 8)
	for i := range out {
		t := float64(i) / float64(rate)
		ci := int(t/chordDur) % len(chords)
		next := (ci + 1) % len(chords)
		// Crossfade into the next chord over the last second.
		u := math.Mod(t, chordDur)
		mix := min(max((u-(chordDur-1))/1, 0), 1)
		v := 0.0
		for k := range 4 {
			for d, det := range []float64{0.997, 1.003} {
				f := (chords[ci][k]*(1-mix) + chords[next][k]*mix) * det
				ph := &phases[2*k+d]
				*ph = math.Mod(*ph+f/float64(rate), 1)
				v += tri(*ph) / 8
			}
		}
		// A slow swell keeps the bed from sounding static.
		v *= 0.75 + 0.25*math.Sin(2*math.Pi*t/(2*chordDur))
		lp += 0.08 * (v - lp)
		out[i] = 0.5 * lp
	}
	fade := int(1.5 * float64(rate))
	for i := range min(fade, n) {
		k := float64(i) / float64(fade)
		out[i] *= k
		out[n-1-i] *= k
	}
	return out
}

func tri(ph float64) float64 { return 4*math.Abs(ph-0.5) - 1 }

func unit(h uint64) float64 { return float64(h>>11) / float64(1<<53) }

// ToPCM converts a float track to 16-bit samples with hard clipping.
func ToPCM(x []float64) []int16 {
	out := make([]int16, len(x))
	for i, v := range x {
		out[i] = int16(min(max(v, -1), 1) * 32767)
	}
	return out
}
