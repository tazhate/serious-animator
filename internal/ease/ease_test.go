package ease

import (
	"math"
	"testing"
)

func TestEndpoints(t *testing.T) {
	for name, f := range map[string]func(float64) float64{
		"smoothstep": Smoothstep, "sine": InOutSine, "cubic": InOutCubic, "outcubic": OutCubic, "outback": OutBack,
	} {
		if v := f(0); math.Abs(v) > 1e-12 {
			t.Errorf("%s(0) = %v", name, v)
		}
		if v := f(1); math.Abs(v-1) > 1e-12 {
			t.Errorf("%s(1) = %v", name, v)
		}
		if f(-1) != f(0) || f(2) != f(1) {
			t.Errorf("%s does not clamp", name)
		}
	}
}

func TestOutBackOvershoots(t *testing.T) {
	peak := 0.0
	for i := range 101 {
		peak = max(peak, OutBack(float64(i)/100))
	}
	if peak < 1.05 || peak > 1.15 {
		t.Fatalf("peak %v, want about 1.1", peak)
	}
}

func TestInOutCubicIsMonotone(t *testing.T) {
	prev := 0.0
	for i := range 101 {
		v := InOutCubic(float64(i) / 100)
		if v < prev {
			t.Fatalf("decreases at %d", i)
		}
		prev = v
	}
}
