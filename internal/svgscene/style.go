package svgscene

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/tazhate/serious-animator/internal/geom"
)

// paint is a parsed fill or stroke value.
type paint struct {
	none bool
	c    color.NRGBA
}

var namedColors = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255},
	"red": {255, 0, 0, 255}, "green": {0, 128, 0, 255}, "blue": {0, 0, 255, 255},
	"yellow": {255, 255, 0, 255}, "orange": {255, 165, 0, 255}, "purple": {128, 0, 128, 255},
	"gray": {128, 128, 128, 255}, "grey": {128, 128, 128, 255}, "brown": {165, 42, 42, 255},
	"pink": {255, 192, 203, 255}, "cyan": {0, 255, 255, 255}, "magenta": {255, 0, 255, 255},
	"lime": {0, 255, 0, 255}, "navy": {0, 0, 128, 255}, "teal": {0, 128, 128, 255},
	"maroon": {128, 0, 0, 255}, "olive": {128, 128, 0, 255}, "silver": {192, 192, 192, 255},
	"gold": {255, 215, 0, 255}, "skyblue": {135, 206, 235, 255}, "lightblue": {173, 216, 230, 255},
	"darkblue": {0, 0, 139, 255}, "darkgreen": {0, 100, 0, 255}, "lightgreen": {144, 238, 144, 255},
	"darkred": {139, 0, 0, 255}, "crimson": {220, 20, 60, 255}, "tomato": {255, 99, 71, 255},
	"coral": {255, 127, 80, 255}, "salmon": {250, 128, 114, 255}, "khaki": {240, 230, 140, 255},
	"beige": {245, 245, 220, 255}, "tan": {210, 180, 140, 255}, "chocolate": {210, 105, 30, 255},
	"sienna": {160, 82, 45, 255}, "violet": {238, 130, 238, 255}, "indigo": {75, 0, 130, 255},
	"turquoise": {64, 224, 208, 255}, "lightgray": {211, 211, 211, 255}, "lightgrey": {211, 211, 211, 255},
	"darkgray": {169, 169, 169, 255}, "darkgrey": {169, 169, 169, 255}, "dimgray": {105, 105, 105, 255},
	"forestgreen": {34, 139, 34, 255}, "steelblue": {70, 130, 180, 255}, "royalblue": {65, 105, 225, 255},
	"dodgerblue": {30, 144, 255, 255}, "goldenrod": {218, 165, 32, 255}, "orangered": {255, 69, 0, 255},
	"limegreen": {50, 205, 50, 255}, "seagreen": {46, 139, 87, 255}, "wheat": {245, 222, 179, 255},
	"ivory": {255, 255, 240, 255}, "lavender": {230, 230, 250, 255}, "plum": {221, 160, 221, 255},
}

func parsePaint(v string) (paint, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch {
	case v == "none" || v == "transparent":
		return paint{none: true}, nil
	case v == "currentcolor":
		return paint{c: color.NRGBA{0, 0, 0, 255}}, nil
	case strings.HasPrefix(v, "url("):
		return paint{}, fmt.Errorf("paint %q: gradients and patterns are not supported, use a flat color", v)
	case strings.HasPrefix(v, "#"):
		return parseHex(v[1:])
	case strings.HasPrefix(v, "rgb"):
		return parseRGB(v)
	}
	if c, ok := namedColors[v]; ok {
		return paint{c: c}, nil
	}
	return paint{}, fmt.Errorf("unknown color %q, use #rrggbb", v)
}

func parseHex(h string) (paint, error) {
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, r := range h {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		h = b.String()
	}
	if len(h) != 6 && len(h) != 8 {
		return paint{}, fmt.Errorf("bad hex color #%s", h)
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return paint{}, fmt.Errorf("bad hex color #%s", h)
	}
	if len(h) == 6 {
		n = n<<8 | 0xff
	}
	return paint{c: color.NRGBA{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), uint8(n)}}, nil
}

func parseRGB(v string) (paint, error) {
	open, close := strings.IndexByte(v, '('), strings.LastIndexByte(v, ')')
	if open < 0 || close < open {
		return paint{}, fmt.Errorf("bad color %q", v)
	}
	parts := strings.FieldsFunc(v[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) != 3 && len(parts) != 4 {
		return paint{}, fmt.Errorf("bad color %q", v)
	}
	var ch [4]uint8
	ch[3] = 255
	for i, p := range parts {
		pct := strings.HasSuffix(p, "%")
		f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
		if err != nil {
			return paint{}, fmt.Errorf("bad color %q", v)
		}
		switch {
		case pct:
			f = f / 100 * 255
		case i == 3:
			f *= 255
		}
		ch[i] = uint8(math.Round(min(max(f, 0), 255)))
	}
	return paint{c: color.NRGBA{ch[0], ch[1], ch[2], ch[3]}}, nil
}

// parseLength accepts plain numbers and "px"; other units are rejected.
func parseLength(v string) (float64, error) {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, "px")
	if strings.HasSuffix(v, "%") || strings.HasSuffix(v, "em") || strings.HasSuffix(v, "pt") {
		return 0, fmt.Errorf("length %q: only plain numbers are supported", v)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("bad number %q", v)
	}
	return f, nil
}

// parseNumbers splits a list like "1,2 3 4" (points, viewBox, transform args).
func parseNumbers(v string) ([]float64, error) {
	var out []float64
	for f := range strings.FieldsFuncSeq(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, fmt.Errorf("bad number %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

// parseTransform parses a transform list: matrix, translate, scale, rotate,
// skewX, skewY.
func parseTransform(v string) (geom.Mat, error) {
	m := geom.Identity
	rest := strings.TrimSpace(v)
	for rest != "" {
		name, args, ok := strings.Cut(rest, "(")
		if !ok {
			return m, fmt.Errorf("bad transform %q", v)
		}
		argStr, after, ok := strings.Cut(args, ")")
		if !ok {
			return m, fmt.Errorf("bad transform %q", v)
		}
		rest = strings.TrimLeft(after, " ,\t\n")
		a, err := parseNumbers(argStr)
		if err != nil {
			return m, fmt.Errorf("transform %q: %w", v, err)
		}
		name = strings.TrimSpace(name)
		var t geom.Mat
		switch {
		case name == "matrix" && len(a) == 6:
			t = geom.Mat{a[0], a[1], a[2], a[3], a[4], a[5]}
		case name == "translate" && len(a) == 1:
			t = geom.Translate(a[0], 0)
		case name == "translate" && len(a) == 2:
			t = geom.Translate(a[0], a[1])
		case name == "scale" && len(a) == 1:
			t = geom.ScaleXY(a[0], a[0])
		case name == "scale" && len(a) == 2:
			t = geom.ScaleXY(a[0], a[1])
		case name == "rotate" && len(a) == 1:
			t = geom.Rotate(a[0])
		case name == "rotate" && len(a) == 3:
			t = geom.Translate(a[1], a[2]).Mul(geom.Rotate(a[0])).Mul(geom.Translate(-a[1], -a[2]))
		case name == "skewX" && len(a) == 1:
			t = geom.Mat{1, 0, math.Tan(a[0] * math.Pi / 180), 1, 0, 0}
		case name == "skewY" && len(a) == 1:
			t = geom.Mat{1, math.Tan(a[0] * math.Pi / 180), 0, 1, 0, 0}
		default:
			return m, fmt.Errorf("unsupported transform %s(%s)", name, argStr)
		}
		m = m.Mul(t)
	}
	return m, nil
}

// parseStyleAttr splits an inline style="a: b; c: d" into declarations.
func parseStyleAttr(v string) map[string]string {
	out := map[string]string{}
	for decl := range strings.SplitSeq(v, ";") {
		k, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return out
}
