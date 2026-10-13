package geom

import "math"

// Cumulative returns the arc length at every vertex; the last value is the
// total length.
func Cumulative(pts []Pt) []float64 {
	cum := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		cum[i] = cum[i-1] + pts[i].Dist(pts[i-1])
	}
	return cum
}

// Prefix returns the part of the polyline from its start up to arc length l,
// with an interpolated end point. cum must come from Cumulative(pts).
func Prefix(pts []Pt, cum []float64, l float64) []Pt {
	if len(pts) == 0 || l <= 0 {
		return nil
	}
	total := cum[len(cum)-1]
	if l >= total {
		return pts
	}
	// First vertex whose cumulative length reaches l.
	lo, hi := 0, len(cum)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if cum[mid] < l {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	out := make([]Pt, lo+1)
	copy(out, pts[:lo])
	seg := cum[lo] - cum[lo-1]
	t := 0.0
	if seg > 0 {
		t = (l - cum[lo-1]) / seg
	}
	out[lo] = pts[lo-1].Lerp(pts[lo], t)
	return out
}

// Resample inserts vertices so that no edge is longer than step. Corners are
// kept, so the shape does not change.
func Resample(pts []Pt, step float64) []Pt {
	if len(pts) < 2 || step <= 0 {
		return pts
	}
	out := []Pt{pts[0]}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		n := int(math.Ceil(a.Dist(b) / step))
		for k := 1; k < n; k++ {
			out = append(out, a.Lerp(b, float64(k)/float64(n)))
		}
		out = append(out, b)
	}
	return out
}

// Roughen displaces vertices sideways with smooth deterministic noise so that
// clean vector lines look hand-drawn. amp is the peak offset, period the noise
// wavelength along the line, both in the polyline's units. A closed polyline
// stays closed: the end point gets the same offset as the start.
func Roughen(pl Polyline, amp, period float64, seed uint64) Polyline {
	if amp <= 0 || len(pl.Pts) < 2 {
		return pl
	}
	pts := Resample(pl.Pts, period/4)
	cum := Cumulative(pts)
	total := cum[len(cum)-1]
	if total == 0 {
		return pl
	}
	// Fade the offset in over the first stretch of an open line so stroke
	// ends stay where the author put them (arrow tips, joins).
	fade := func(s float64) float64 {
		if pl.Closed {
			return 1
		}
		e := min(s, total-s)
		return min(1, e/(period/2))
	}
	out := make([]Pt, len(pts))
	for i, p := range pts {
		n := normalAt(pts, i)
		s := cum[i]
		nz := valueNoise(s/period, seed)
		if pl.Closed {
			// Blend toward the start's noise over the last wavelength so the
			// loop closes without a kink.
			w := min(max((s-(total-period))/period, 0), 1)
			nz += (valueNoise((s-total)/period, seed) - nz) * w
		}
		off := amp * fade(s) * nz
		out[i] = p.Add(n.Mul(off))
	}
	if pl.Closed {
		out[len(out)-1] = out[0]
	}
	return Polyline{Pts: out, Closed: pl.Closed}
}

// normalAt is the unit normal of the polyline at vertex i.
func normalAt(pts []Pt, i int) Pt {
	a := pts[max(i-1, 0)]
	b := pts[min(i+1, len(pts)-1)]
	d := b.Sub(a)
	l := d.Len()
	if l == 0 {
		return Pt{}
	}
	return Pt{-d.Y / l, d.X / l}
}

// valueNoise is 1-D smooth noise in [-1, 1] with integer lattice spacing.
func valueNoise(x float64, seed uint64) float64 {
	i := math.Floor(x)
	f := x - i
	a := lattice(int64(i), seed)
	b := lattice(int64(i)+1, seed)
	f = f * f * (3 - 2*f)
	return a + (b-a)*f
}

func lattice(i int64, seed uint64) float64 {
	h := Hash64(uint64(i) ^ seed*0x9E3779B97F4A7C15)
	return float64(h>>11)/float64(1<<53)*2 - 1
}

// Hash64 is the splitmix64 finalizer: a cheap, well-mixed integer hash.
func Hash64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// HashString hashes a string with FNV-1a and mixes the result.
func HashString(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return Hash64(h)
}
