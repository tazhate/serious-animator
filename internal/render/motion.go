package render

import (
	"math"

	"github.com/tazhate/serious-animator/internal/ease"
	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/svgscene"
)

// motion moves a drawn group: it spins around a point, slides, or travels
// along a guide path. Times are scene-local seconds.
type motion struct {
	src   *svgscene.Motion
	start float64 // set when the scene is scheduled

	guide  []geom.Pt
	cum    []float64
	angle0 float64
}

func newMotion(m *svgscene.Motion) *motion {
	mo := &motion{src: m, start: math.Inf(1)}
	if m.Path != nil {
		for _, pl := range m.Path.Flatten(flattenTol) {
			mo.guide = append(mo.guide, pl.Pts...)
		}
		if len(mo.guide) >= 2 {
			mo.cum = geom.Cumulative(mo.guide)
			mo.angle0 = mo.angleAt(0)
		} else {
			mo.guide = nil
		}
	}
	return mo
}

// moving reports whether the transform changes around time t, so the group
// cannot be cached as a still layer.
func (mo *motion) moving(t float64) bool {
	if mo == nil || t <= mo.start {
		return false
	}
	return mo.src.Spin != 0 || t < mo.start+mo.src.Dur
}

// mat is the viewBox-space transform at time t.
func (mo *motion) mat(t float64) geom.Mat {
	if mo == nil || t <= mo.start {
		return geom.Identity
	}
	dt := t - mo.start
	m := geom.Identity
	k := ease.InOutSine(dt / mo.src.Dur)
	switch {
	case mo.guide != nil:
		total := mo.cum[len(mo.cum)-1]
		s := total * k
		part := geom.Prefix(mo.guide, mo.cum, s)
		p, p0 := mo.guide[0], mo.guide[0]
		if len(part) > 0 {
			p = part[len(part)-1]
		}
		m = geom.Translate(p.X, p.Y)
		if mo.src.Orient {
			m = m.Mul(geom.Rotate(mo.angleAt(s) - mo.angle0))
		}
		m = m.Mul(geom.Translate(-p0.X, -p0.Y))
	case mo.src.Delta != (geom.Pt{}):
		m = geom.Translate(mo.src.Delta.X*k, mo.src.Delta.Y*k)
	}
	if mo.src.Spin != 0 {
		o := mo.src.Origin
		if mo.guide != nil || mo.src.Delta != (geom.Pt{}) {
			o = m.Apply(o)
		}
		spin := geom.Translate(o.X, o.Y).Mul(geom.Rotate(mo.src.Spin * dt)).Mul(geom.Translate(-o.X, -o.Y))
		m = spin.Mul(m)
	}
	return m
}

// angleAt is the guide's direction in degrees at arc length s.
func (mo *motion) angleAt(s float64) float64 {
	i := 1
	for i < len(mo.cum)-1 && mo.cum[i] < s {
		i++
	}
	d := mo.guide[i].Sub(mo.guide[i-1])
	return math.Atan2(d.Y, d.X) * 180 / math.Pi
}
