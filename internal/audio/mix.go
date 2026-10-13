package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tazhate/serious-animator/internal/xexec"
)

// MixRate is the sample rate of the final soundtrack.
const MixRate = 48000

// Loudness targets for the final mix: YouTube's -14 LUFS with headroom.
const (
	targetLUFS = -14.0
	targetTP   = -1.5
	targetLRA  = 11.0
)

// Mix describes the soundtrack layers. Voice is required; the rest may be
// empty.
type Mix struct {
	Voice     string  // narration WAV, sets the length
	SFX       string  // effects WAV
	Music     string  // music file
	LoopMusic bool    // repeat the music file until the narration ends
	MusicDB   float64 // music level before ducking, e.g. -20
	Duration  float64 // seconds
}

// MixGraph builds the filtergraph and input arguments. The music ducks under
// the voice through a sidechain compressor keyed by the narration.
func (m Mix) MixGraph() (inputs []string, graph string) {
	inputs = []string{"-i", m.Voice}
	fmtA := fmt.Sprintf("aformat=sample_fmts=fltp:sample_rates=%d:channel_layouts=mono", MixRate)
	var parts []string
	labels := []string{"[v]"}
	parts = append(parts, fmt.Sprintf("[0:a]%s,asplit=2[v][vk]", fmtA))
	idx := 1
	if m.SFX != "" {
		inputs = append(inputs, "-i", m.SFX)
		parts = append(parts, fmt.Sprintf("[%d:a]%s[fx]", idx, fmtA))
		labels = append(labels, "[fx]")
		idx++
	}
	if m.Music != "" {
		if m.LoopMusic {
			inputs = append(inputs, "-stream_loop", "-1")
		}
		inputs = append(inputs, "-i", m.Music)
		fadeOut := max(m.Duration-2.5, 0)
		parts = append(parts,
			fmt.Sprintf("[%d:a]%s,atrim=0:%.3f,volume=%.1fdB,afade=t=in:d=2,afade=t=out:st=%.3f:d=2.5[m0]",
				idx, fmtA, m.Duration, m.MusicDB, fadeOut),
			"[m0][vk]sidechaincompress=threshold=0.02:ratio=6:attack=40:release=600[m]")
		labels = append(labels, "[m]")
	} else {
		parts = append(parts, "[vk]anullsink")
	}
	parts = append(parts, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0[mix]", strings.Join(labels, ""), len(labels)))
	return inputs, strings.Join(parts, ";")
}

// loudness is the part of loudnorm's JSON report the second pass needs.
type loudness struct {
	I      string `json:"input_i"`
	TP     string `json:"input_tp"`
	LRA    string `json:"input_lra"`
	Thresh string `json:"input_thresh"`
	Offset string `json:"target_offset"`
}

// Render mixes the layers into a WAV at out, normalized in two passes: the
// first measures loudness, the second applies it linearly so the voice keeps
// its dynamics.
func (m Mix) Render(ctx context.Context, out string) error {
	inputs, graph := m.MixGraph()
	norm := fmt.Sprintf("loudnorm=I=%.1f:TP=%.1f:LRA=%.1f", targetLUFS, targetTP, targetLRA)

	args := append([]string{"-nostdin", "-hide_banner", "-v", "info"}, inputs...)
	args = append(args, "-filter_complex", graph+";[mix]"+norm+":print_format=json[out]", "-map", "[out]", "-f", "null", "-")
	log, err := xexec.RunLog(ctx, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("measure loudness: %w", err)
	}
	var l loudness
	if i := strings.LastIndex(log, "{"); i >= 0 {
		if j := strings.LastIndex(log, "}"); j > i {
			err = json.Unmarshal([]byte(log[i:j+1]), &l)
		}
	}
	if err != nil || l.I == "" {
		return errors.Join(errors.New("measure loudness: no loudnorm report from ffmpeg"), err)
	}
	second := fmt.Sprintf("%s:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true",
		norm, l.I, l.TP, l.LRA, l.Thresh, l.Offset)
	args = append([]string{"-nostdin", "-y", "-v", "error"}, inputs...)
	args = append(args, "-filter_complex", graph+";[mix]"+second+"[out]", "-map", "[out]",
		"-ar", fmt.Sprint(MixRate), "-c:a", "pcm_s16le", out)
	if _, err := xexec.Run(ctx, "ffmpeg", args...); err != nil {
		return fmt.Errorf("mix soundtrack: %w", err)
	}
	return nil
}
