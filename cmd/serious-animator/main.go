// Command serious-animator renders hand-drawn whiteboard explainer videos
// from a storyboard and SVG scenes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/tazhate/serious-animator/assets"
	"github.com/tazhate/serious-animator/internal/project"
	"github.com/tazhate/serious-animator/internal/render"
	"github.com/tazhate/serious-animator/internal/tts"
	"github.com/tazhate/serious-animator/internal/xexec"
)

const usage = `serious-animator - narrated whiteboard explainer videos from SVG scenes

Usage:
  serious-animator validate [--json] DIR
  serious-animator preview  [--quality Q] [--out DIR] [--beats=false] [--frames] DIR
  serious-animator render   [-o FILE] [--quality Q] [--no-audio] [--no-music] [--no-sfx]
                            [--no-word-sync] [--roughness R] [--no-marker] [--jobs N] DIR
  serious-animator voices   [--lang ru]
  serious-animator doctor

DIR is a project directory with storyboard.json and the scene SVGs.
Quality presets: low (480p20), medium (720p24, default), high (1080p30).
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmds := map[string]func(context.Context, []string, io.Writer, io.Writer) error{
		"validate": cmdValidate,
		"preview":  cmdPreview,
		"render":   cmdRender,
		"voices":   cmdVoices,
		"doctor":   cmdDoctor,
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	cmd, ok := cmds[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	err := cmd(ctx, args[1:], stdout, stderr)
	_, isUsage := errors.AsType[usageError](err)
	switch {
	case err == nil:
		return 0
	case isUsage:
		fmt.Fprintf(stderr, "%v\n\n%s", err, usage)
		return 2
	case errors.Is(err, errInvalid):
		return 1
	default:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// errInvalid means validation problems were already printed.
var errInvalid = errors.New("project is invalid")

// parse handles flags placed before or after the positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{fmt.Sprintf("%s: %v", fs.Name(), err)}
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func oneDir(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parse(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", usageError{fmt.Sprintf("%s: want exactly one project directory, got %d arguments", fs.Name(), len(pos))}
	}
	return pos[0], nil
}

// load reads a project and prints its problems. It fails on errors.
func load(dir string, stderr io.Writer) (*project.Project, error) {
	proj, probs, err := project.Load(dir)
	if err != nil {
		return nil, err
	}
	for _, p := range probs {
		fmt.Fprintln(stderr, p)
	}
	if project.HasErrors(probs) {
		fmt.Fprintln(stderr, "fix the errors above, then run the command again")
		return nil, errInvalid
	}
	return proj, nil
}

func cmdValidate(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print problems as JSON")
	dir, err := oneDir(fs, args)
	if err != nil {
		return err
	}
	proj, probs, err := project.Load(dir)
	if err != nil {
		return err
	}
	nErr := 0
	for _, p := range probs {
		if p.Error {
			nErr++
		}
	}
	if *asJSON {
		out := struct {
			OK       bool              `json:"ok"`
			Scenes   int               `json:"scenes"`
			Beats    int               `json:"beats"`
			Problems []project.Problem `json:"problems"`
		}{OK: nErr == 0, Scenes: len(proj.Board.Scenes), Problems: probs}
		for _, s := range proj.Board.Scenes {
			out.Beats += len(s.Beats)
		}
		if out.Problems == nil {
			out.Problems = []project.Problem{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		for _, p := range probs {
			fmt.Fprintln(stdout, p)
		}
		fmt.Fprintf(stdout, "%d errors, %d warnings\n", nErr, len(probs)-nErr)
	}
	if nErr > 0 {
		return errInvalid
	}
	return nil
}

func quality(name string) (render.Quality, error) {
	q, ok := render.Qualities[name]
	if !ok {
		return q, usageError{fmt.Sprintf("unknown quality %q, want low, medium or high", name)}
	}
	return q, nil
}

func cmdPreview(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	qName := fs.String("quality", "medium", "resolution preset")
	outDir := fs.String("out", "", "output directory (default DIR/preview)")
	beats := fs.Bool("beats", true, "outline and number each beat's area")
	frames := fs.Bool("frames", false, "camera scenes: one image per beat, as the camera frames it")
	rough := fs.Float64("roughness", 1, "hand-drawn wobble, 0 for exact lines")
	dir, err := oneDir(fs, args)
	if err != nil {
		return err
	}
	q, err := quality(*qName)
	if err != nil {
		return err
	}
	proj, err := load(dir, stderr)
	if err != nil {
		return err
	}
	if *outDir == "" {
		*outDir = filepath.Join(dir, "preview")
	}
	paths, err := render.Preview(proj, *outDir, render.PreviewOptions{Quality: q, Roughness: *rough, Beats: *beats, Frames: *frames})
	for _, p := range paths {
		fmt.Fprintln(stdout, p)
	}
	return err
}

func cmdRender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	out := fs.String("o", "", "output MP4 (default DIR/video.mp4)")
	qName := fs.String("quality", "medium", "resolution preset")
	noAudio := fs.Bool("no-audio", false, "skip narration; time beats by text length")
	rough := fs.Float64("roughness", 1, "hand-drawn wobble, 0 for exact lines")
	noMarker := fs.Bool("no-marker", false, "hide the drawing marker")
	noMusic := fs.Bool("no-music", false, "leave out the storyboard's music")
	noSFX := fs.Bool("no-sfx", false, "leave out pops, whooshes and the marker sound")
	noWords := fs.Bool("no-word-sync", false, "start drawing at the sentence, not at the label word")
	jobs := fs.Int("jobs", 0, "frames rendered in parallel (default: CPU count, at most 8)")
	cacheDir := fs.String("cache", tts.DefaultCacheDir(), "narration cache directory")
	verbose := fs.Bool("v", false, "verbose progress")
	dir, err := oneDir(fs, args)
	if err != nil {
		return err
	}
	q, err := quality(*qName)
	if err != nil {
		return err
	}
	if *rough < 0 || *rough > 5 {
		return usageError{"--roughness must be between 0 and 5"}
	}
	if *out == "" {
		*out = filepath.Join(dir, "video.mp4")
	}
	if !strings.EqualFold(filepath.Ext(*out), ".mp4") {
		return usageError{"-o must end in .mp4"}
	}
	proj, err := load(dir, stderr)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	res, err := render.Video(ctx, proj, *out, render.Options{
		Quality:   q,
		Roughness: *rough,
		Marker:    !*noMarker,
		NoAudio:   *noAudio,
		NoMusic:   *noMusic,
		NoSFX:     *noSFX,
		NoWords:   *noWords,
		Jobs:      *jobs,
		TTS:       tts.Cached{Engine: tts.EdgeTTS{NoWords: *noWords}, Dir: *cacheDir},
		Log:       log,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s\n%d scenes, %s, %d frames\n", res.Path, res.Scenes, res.Duration.Round(100*time.Millisecond), res.Frames)
	return nil
}

func cmdVoices(ctx context.Context, args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("voices", flag.ContinueOnError)
	lang := fs.String("lang", "", "language prefix, e.g. ru or en-US")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	out, err := xexec.Run(ctx, "edge-tts", "--list-voices")
	if err != nil {
		return err
	}
	prefix := strings.ToLower(*lang)
	for line := range strings.Lines(string(out)) {
		l := strings.ToLower(line)
		if prefix == "" || strings.HasPrefix(l, prefix) || strings.HasPrefix(l, "name") || strings.HasPrefix(l, "---") {
			if _, err := fmt.Fprint(stdout, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func cmdDoctor(ctx context.Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 0 {
		return usageError{"doctor takes no arguments"}
	}
	ok := true
	check := func(name string, err error, detail string) {
		mark := "ok  "
		if err != nil {
			mark, ok = "FAIL", false
			detail = err.Error()
		}
		fmt.Fprintf(stdout, "%s %-9s %s\n", mark, name, detail)
	}
	for _, bin := range []string{"ffmpeg", "edge-tts"} {
		path, err := exec.LookPath(bin)
		if err != nil {
			check(bin, err, "")
			continue
		}
		ver := path
		vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		flagArg := "-version"
		if bin == "edge-tts" {
			flagArg = "--version"
		}
		if b, err := xexec.Run(vctx, bin, flagArg); err == nil {
			first, _, _ := strings.Cut(string(b), "\n")
			ver = strings.TrimSpace(first)
		}
		cancel()
		check(bin, nil, ver)
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		b, err := xexec.Run(ctx, path, "-hide_banner", "-encoders")
		if err == nil && !slices.ContainsFunc(strings.Fields(string(b)), func(s string) bool { return s == "libx264" }) {
			err = errors.New("ffmpeg has no libx264 encoder")
		}
		check("libx264", err, "available")
	}
	if py, err := tts.WordTiming(); err != nil {
		fmt.Fprintf(stdout, "warn %-9s %v; drawing starts at the sentence, not at the label word\n", "words", err)
	} else {
		check("words", nil, "edge_tts importable by "+py)
	}
	_, err := assets.Font()
	check("font", err, "Caveat embedded")
	if !ok {
		return errors.New("some checks failed")
	}
	return nil
}
