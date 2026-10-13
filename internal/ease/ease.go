// Package ease holds the timing curves used by every animation: progress in
// [0, 1] goes in, eased progress comes out.
package ease

import "math"

// Clamp01 limits t to [0, 1].
func Clamp01(t float64) float64 { return min(max(t, 0), 1) }

// Smoothstep starts and ends with zero speed.
func Smoothstep(t float64) float64 {
	t = Clamp01(t)
	return t * t * (3 - 2*t)
}

// InOutSine is a gentle ease for long strokes: it never stalls in the middle.
func InOutSine(t float64) float64 {
	return 0.5 - 0.5*math.Cos(math.Pi*Clamp01(t))
}

// InOutCubic is the ease for camera moves and transitions.
func InOutCubic(t float64) float64 {
	t = Clamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
}

// OutCubic decelerates into the end value.
func OutCubic(t float64) float64 {
	u := 1 - Clamp01(t)
	return 1 - u*u*u
}

// OutBack overshoots the end value by about 10% and settles back: a pop.
func OutBack(t float64) float64 {
	const c1 = 1.70158
	const c3 = c1 + 1
	u := Clamp01(t) - 1
	return 1 + c3*u*u*u + c1*u*u
}
