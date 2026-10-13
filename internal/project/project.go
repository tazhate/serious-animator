// Package project loads a video project (storyboard.json plus one SVG per
// scene) and validates everything that can be checked before rendering.
package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tazhate/serious-animator/internal/geom"
	"github.com/tazhate/serious-animator/internal/svgscene"
)

// File is the storyboard file name inside a project directory.
const File = "storyboard.json"

// Limits enforced by Validate.
const (
	MinScenes       = 1
	MaxScenes       = 12
	MinBeats        = 2
	MaxBeats        = 8
	MaxNarration    = 300 // runes
	MaxLabel        = 40  // runes
	maxItemsWarning = 400
)

// Storyboard is the script of the video.
type Storyboard struct {
	Title  string  `json:"title"`
	Lang   string  `json:"lang,omitempty"`
	Voice  string  `json:"voice,omitempty"`
	Rate   string  `json:"rate,omitempty"`
	Scenes []Scene `json:"scenes"`

	// Music is a project-relative audio file or "generated"; empty for none.
	Music string `json:"music,omitempty"`
	// MusicVolume is the music level in dB, -20 when unset.
	MusicVolume *float64 `json:"music_volume,omitempty"`
	// SFX switches the pop and whoosh sounds, on when unset.
	SFX *bool `json:"sfx,omitempty"`
}

// Scene is one drawing with its narration beats.
type Scene struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	SVG   string `json:"svg"`
	Beats []Beat `json:"beats"`

	// Camera is "auto" (follow the beats), "off", or empty: auto when the
	// board is larger than one 1600x900 frame.
	Camera string `json:"camera,omitempty"`
	// Dim fades the elements of earlier beats while a new beat is drawn.
	Dim bool `json:"dim,omitzero"`
	// Transition into the next scene: "swipe" (default), "fade" or "cut".
	Transition string `json:"transition,omitempty"`
}

// Beat is one spoken sentence and the element drawn while it is spoken.
type Beat struct {
	Label     string `json:"label"`
	Narration string `json:"narration"`
	// Pause adds silence after the sentence, seconds.
	Pause float64 `json:"pause,omitzero"`
	// Camera overrides the framing: "board", "hold" or "#element-id".
	Camera string `json:"camera,omitempty"`
}

// Limits for the optional fields.
const (
	MaxPause        = 3.0
	DefaultMusicDB  = -20.0
	MinMusicDB      = -40.0
	MaxMusicDB      = -6.0
	MusicGenerated  = "generated"
	CameraAuto      = "auto"
	CameraOff       = "off"
	TransitionSwipe = "swipe"
	TransitionFade  = "fade"
	TransitionCut   = "cut"
	BeatCameraBoard = "board"
	BeatCameraHold  = "hold"
)

var musicExts = []string{".mp3", ".wav", ".ogg", ".m4a", ".flac", ".opus"}

// MusicDB returns the music level with the default applied.
func (b *Storyboard) MusicDB() float64 {
	if b.MusicVolume == nil {
		return DefaultMusicDB
	}
	return *b.MusicVolume
}

// SFXOn reports whether sound effects are wanted.
func (b *Storyboard) SFXOn() bool { return b.SFX == nil || *b.SFX }

// Project is a loaded storyboard with its parsed drawings. Drawings[i] is nil
// when scene i's SVG could not be read.
type Project struct {
	Dir      string
	Board    Storyboard
	Drawings []*svgscene.Scene
}

// Problem is a validation finding.
type Problem struct {
	File  string `json:"file"`
	Line  int    `json:"line,omitzero"`
	Error bool   `json:"error"`
	Msg   string `json:"message"`
}

func (p Problem) String() string {
	lvl := "warning"
	if p.Error {
		lvl = "error"
	}
	loc := p.File
	if p.Line > 0 {
		loc = fmt.Sprintf("%s:%d", p.File, p.Line)
	}
	return fmt.Sprintf("%s: %s: %s", lvl, loc, p.Msg)
}

// HasErrors reports whether any problem is an error.
func HasErrors(ps []Problem) bool {
	for _, p := range ps {
		if p.Error {
			return true
		}
	}
	return false
}

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	voiceRe = regexp.MustCompile(`^[A-Za-z]{2,3}-[A-Za-z0-9]+-[A-Za-z0-9]+$`)
	rateRe  = regexp.MustCompile(`^[+-][0-9]{1,3}%$`)
)

// DefaultVoice picks an edge-tts voice for a language code.
func DefaultVoice(lang string) string {
	switch strings.ToLower(lang) {
	case "ru":
		return "ru-RU-SvetlanaNeural"
	case "uk":
		return "uk-UA-OstapNeural"
	case "de":
		return "de-DE-ConradNeural"
	case "es":
		return "es-ES-AlvaroNeural"
	case "fr":
		return "fr-FR-HenriNeural"
	}
	return "en-US-AndrewMultilingualNeural"
}

// Load reads and validates a project. The error is returned only when the
// storyboard itself cannot be read; everything else is a Problem.
func Load(dir string) (*Project, []Problem, error) {
	raw, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	p := &Project{Dir: dir}
	if err := dec.Decode(&p.Board); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", File, err)
	}
	if p.Board.Voice == "" {
		p.Board.Voice = DefaultVoice(p.Board.Lang)
	}
	if p.Board.Rate == "" {
		p.Board.Rate = "+0%"
	}
	probs := p.validateBoard()
	p.Drawings = make([]*svgscene.Scene, len(p.Board.Scenes))
	for i := range p.Board.Scenes {
		probs = append(probs, p.loadDrawing(i)...)
	}
	return p, probs, nil
}

func (p *Project) validateBoard() []Problem {
	var ps []Problem
	errf := func(format string, a ...any) {
		ps = append(ps, Problem{File: File, Error: true, Msg: fmt.Sprintf(format, a...)})
	}
	warnf := func(format string, a ...any) {
		ps = append(ps, Problem{File: File, Msg: fmt.Sprintf(format, a...)})
	}
	b := &p.Board
	if strings.TrimSpace(b.Title) == "" {
		errf("title is empty")
	}
	if !voiceRe.MatchString(b.Voice) {
		errf("voice %q does not look like an edge-tts voice name (e.g. ru-RU-DmitryNeural)", b.Voice)
	}
	if !rateRe.MatchString(b.Rate) {
		errf("rate %q must look like +0%% or -10%%", b.Rate)
	}
	switch {
	case b.Music == "", b.Music == MusicGenerated:
	case checkLocal(b.Music) != nil:
		errf("music: %v", checkLocal(b.Music))
	case !slices.Contains(musicExts, strings.ToLower(filepath.Ext(b.Music))):
		errf("music: %q must be one of %s, or %q", b.Music, strings.Join(musicExts, " "), MusicGenerated)
	default:
		if _, err := os.Stat(filepath.Join(p.Dir, b.Music)); err != nil {
			errf("music: %v", err)
		}
	}
	if v := b.MusicDB(); v < MinMusicDB || v > MaxMusicDB {
		errf("music_volume %v dB, want %v to %v", v, MinMusicDB, MaxMusicDB)
	}
	if n := len(b.Scenes); n < MinScenes || n > MaxScenes {
		errf("%d scenes, want %d-%d", n, MinScenes, MaxScenes)
	}
	seen := map[string]bool{}
	for si, sc := range b.Scenes {
		where := fmt.Sprintf("scene %d (%s)", si+1, sc.ID)
		switch {
		case !idRe.MatchString(sc.ID):
			errf("%s: id must match %s", where, idRe)
		case seen[sc.ID]:
			errf("%s: duplicate id", where)
		}
		seen[sc.ID] = true
		if err := checkRelPath(sc.SVG); err != nil {
			errf("%s: svg: %v", where, err)
		}
		if !slices.Contains([]string{"", CameraAuto, CameraOff}, sc.Camera) {
			errf("%s: camera %q, want %q or %q", where, sc.Camera, CameraAuto, CameraOff)
		}
		if !slices.Contains([]string{"", TransitionSwipe, TransitionFade, TransitionCut}, sc.Transition) {
			errf("%s: transition %q, want swipe, fade or cut", where, sc.Transition)
		}
		if n := len(sc.Beats); n < MinBeats || n > MaxBeats {
			errf("%s: %d beats, want %d-%d", where, n, MinBeats, MaxBeats)
		}
		for bi, bt := range sc.Beats {
			bw := fmt.Sprintf("%s beat %d", where, bi+1)
			if n := utf8.RuneCountInString(strings.TrimSpace(bt.Narration)); n == 0 || n > MaxNarration {
				errf("%s: narration has %d characters, want 1-%d", bw, n, MaxNarration)
			}
			if n := utf8.RuneCountInString(strings.TrimSpace(bt.Label)); n == 0 || n > MaxLabel {
				errf("%s: label has %d characters, want 1-%d", bw, n, MaxLabel)
			}
			if bt.Pause < 0 || bt.Pause > MaxPause {
				errf("%s: pause %v, want 0-%v seconds", bw, bt.Pause, MaxPause)
			}
			if c := bt.Camera; c != "" && c != BeatCameraBoard && c != BeatCameraHold && !strings.HasPrefix(c, "#") {
				errf("%s: camera %q, want board, hold or #element-id", bw, c)
			}
			if missing := UnspokenWords(bt.Label, bt.Narration); len(missing) > 0 {
				warnf("%s: label word(s) %q are not spoken in the narration; the viewer reads the caption while hearing the sentence", bw, missing)
			}
		}
	}
	return ps
}

// checkRelPath keeps scene files inside the project directory.
func checkRelPath(p string) error {
	if err := checkLocal(p); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(p), ".svg") {
		return errors.New("path must end in .svg")
	}
	return nil
}

// checkLocal keeps a referenced file inside the project directory.
func checkLocal(p string) error {
	switch {
	case p == "":
		return errors.New("path is empty")
	case filepath.IsAbs(p):
		return errors.New("path must be relative to the project directory")
	case !filepath.IsLocal(p):
		return errors.New("path must stay inside the project directory")
	}
	return nil
}

func (p *Project) loadDrawing(i int) []Problem {
	sc := p.Board.Scenes[i]
	if checkRelPath(sc.SVG) != nil {
		return nil // already reported
	}
	file := sc.SVG
	var ps []Problem
	add := func(line int, isErr bool, format string, a ...any) {
		ps = append(ps, Problem{File: file, Line: line, Error: isErr, Msg: fmt.Sprintf(format, a...)})
	}
	f, err := os.Open(filepath.Join(p.Dir, sc.SVG))
	if err != nil {
		add(0, true, "%v", err)
		return ps
	}
	defer f.Close()
	d, issues, err := svgscene.Parse(f)
	for _, is := range issues {
		add(is.Line, is.Error, "%s", is.Msg)
	}
	if err != nil {
		add(0, true, "%v", err)
		return ps
	}
	p.Drawings[i] = d

	nb := len(sc.Beats)
	perBeat := make([]int, nb)
	for _, it := range d.Items {
		switch {
		case it.Beat == 0:
			add(it.Line, true, "<%s id=%q> has no data-beat (set it on the element or a parent <g>)", it.Tag, it.ID)
		case it.Beat > nb:
			add(it.Line, true, "<%s id=%q> has data-beat=%d but the scene has %d beats", it.Tag, it.ID, it.Beat, nb)
		default:
			perBeat[it.Beat-1]++
		}
	}
	for _, h := range d.Highlights {
		switch {
		case h.Beat > nb:
			add(h.Line, true, "data-highlight beat %d, but the scene has %d beats", h.Beat, nb)
		default:
			perBeat[h.Beat-1]++
		}
	}
	for bi, bt := range sc.Beats {
		if id, ok := strings.CutPrefix(bt.Camera, "#"); ok {
			if _, found := d.Boxes[id]; !found {
				add(0, true, "beat %d camera %q: no element or group with that id", bi+1, bt.Camera)
			}
		}
	}
	for _, m := range d.Motions {
		if m.Beat > nb {
			add(m.Line, true, "data-motion-beat %d, but the scene has %d beats", m.Beat, nb)
		}
	}
	if CameraOn(sc, d.ViewBox) {
		if r := d.ViewBox.W() / d.ViewBox.H(); math.Abs(r-16.0/9) > 0.02 {
			add(0, false, "the camera frames a 16:9 view; a %.0fx%.0f board leaves margins", d.ViewBox.W(), d.ViewBox.H())
		}
		if d.ViewBox.W() > 4800 {
			add(0, false, "board is %.0f wide; camera moves get long, keep it under 4800", d.ViewBox.W())
		}
	}
	for bi, n := range perBeat {
		if n == 0 {
			add(0, true, "beat %d (%q) has nothing to draw; give at least one element data-beat=\"%d\"", bi+1, sc.Beats[bi].Label, bi+1)
		}
	}
	if len(d.Items) > maxItemsWarning {
		add(0, false, "%d elements; rendering slows down, merge small details into fewer paths", len(d.Items))
	}
	ps = append(ps, layoutWarnings(file, d, sc)...)
	return ps
}

// layoutWarnings flags elements outside the canvas, captions that never made
// it into the drawing, and elements of different beats piled on each other.
func layoutWarnings(file string, d *svgscene.Scene, sc Scene) []Problem {
	var ps []Problem
	vb := d.ViewBox
	margin := 0.02 * max(vb.W(), vb.H())
	outer := vb.Inset(margin)
	for _, it := range d.Items {
		b := it.Bounds()
		if b.Min.X < outer.Min.X || b.Min.Y < outer.Min.Y || b.Max.X > outer.Max.X || b.Max.Y > outer.Max.Y {
			ps = append(ps, Problem{File: file, Line: it.Line, Msg: fmt.Sprintf("<%s id=%q> sticks out of the viewBox and will be cut off", it.Tag, it.ID)})
		}
	}

	// Caption check: the label should appear as text drawn in its own beat.
	for bi, bt := range sc.Beats {
		var texts []string
		for _, it := range d.Items {
			if it.IsText() && it.Beat == bi+1 {
				texts = append(texts, it.Text)
			}
		}
		if len(texts) > 0 && !containsFold(strings.Join(texts, " "), bt.Label) {
			ps = append(ps, Problem{File: file, Msg: fmt.Sprintf("beat %d label %q is not among the texts drawn in that beat %q", bi+1, bt.Label, texts)})
		}
	}

	// Overlap check on solid elements (fills and text) only: arrows and
	// outlines are meant to touch the things they connect.
	const thin = 15.0
	type box struct {
		it *svgscene.Item
		r  geom.Rect
	}
	var boxes []box
	for i := range d.Items {
		it := &d.Items[i]
		r := it.Bounds()
		if (it.HasFill || it.IsText()) && min(r.W(), r.H()) >= thin {
			boxes = append(boxes, box{it, r})
		}
	}
	reported := 0
	for i := 0; i < len(boxes) && reported < 10; i++ {
		for j := i + 1; j < len(boxes) && reported < 10; j++ {
			a, b := boxes[i], boxes[j]
			if a.it.Beat == b.it.Beat {
				continue
			}
			inter := a.r.Intersect(b.r).Area()
			if inter > 0.25*min(a.r.Area(), b.r.Area()) {
				ps = append(ps, Problem{File: file, Line: b.it.Line, Msg: fmt.Sprintf(
					"<%s id=%q> (beat %d) overlaps <%s id=%q> (beat %d); spread elements apart",
					b.it.Tag, b.it.ID, b.it.Beat, a.it.Tag, a.it.ID, a.it.Beat)})
				reported++
			}
		}
	}
	return ps
}

func containsFold(hay, needle string) bool {
	return strings.Contains(normalize(hay), normalize(needle))
}

func joinWords(w []string) string { return strings.Join(w, " ") }

// FrameW and FrameH are the size of one camera frame on the board.
const (
	FrameW = 1600.0
	FrameH = 900.0
)

// CameraOn reports whether a scene is filmed with a moving camera: when asked
// for, or when the board is clearly larger than one frame.
func CameraOn(sc Scene, vb geom.Rect) bool {
	switch sc.Camera {
	case CameraAuto:
		return true
	case CameraOff:
		return false
	}
	return vb.W()*vb.H() > 1.2*FrameW*FrameH
}
