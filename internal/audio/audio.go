// Package audio handles the narration track as 16-bit mono PCM: decoding
// through ffmpeg, trimming silence, assembling clips and writing WAV.
package audio

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tazhate/serious-animator/internal/xexec"
)

// Rate is the sample rate of every clip and of the final track.
const Rate = 24000

// Decode converts any audio file ffmpeg understands into mono PCM at Rate.
// The format is forced on output, so whatever the TTS returned (mp3, wav,
// another sample rate) ends up the same.
func Decode(ctx context.Context, file string) ([]int16, error) {
	out, err := xexec.Run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", file,
		"-f", "s16le", "-acodec", "pcm_s16le", "-ac", "1", "-ar", fmt.Sprint(Rate), "-")
	if err != nil {
		return nil, err
	}
	return FromBytes(out), nil
}

// FromBytes reads little-endian int16 samples; a trailing odd byte is dropped.
func FromBytes(b []byte) []int16 {
	pcm := make([]int16, len(b)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return pcm
}

// ToBytes is the inverse of FromBytes.
func ToBytes(pcm []int16) []byte {
	b := make([]byte, 2*len(pcm))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(s))
	}
	return b
}

// silenceLevel is -45 dBFS: quieter samples count as silence when trimming.
const silenceLevel = 184

// TrimSilence cuts leading and trailing silence, leaving pad samples on each
// side so consonants at the edges are not clipped. TTS engines add uneven
// silence around clips; trimming makes the gaps between beats uniform.
func TrimSilence(pcm []int16, pad int) []int16 {
	out, _ := TrimSilenceOffset(pcm, pad)
	return out
}

// TrimSilenceOffset is TrimSilence that also returns how many samples were
// cut from the start, so timings inside the clip can be shifted.
func TrimSilenceOffset(pcm []int16, pad int) ([]int16, int) {
	loud := func(s int16) bool { return s > silenceLevel || s < -silenceLevel }
	first, last := -1, -1
	for i, s := range pcm {
		if loud(s) {
			first = i
			break
		}
	}
	if first < 0 {
		return nil, 0
	}
	for i := len(pcm) - 1; i >= 0; i-- {
		if loud(pcm[i]) {
			last = i
			break
		}
	}
	start := max(first-pad, 0)
	return pcm[start:min(last+pad+1, len(pcm))], start
}

// WriteWAV writes a mono 16-bit PCM WAV file.
func WriteWAV(path string, pcm []int16, rate int) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	dataLen := uint32(2 * len(pcm))
	hdr := []any{
		[4]byte{'R', 'I', 'F', 'F'}, 36 + dataLen, [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1),
		uint32(rate), uint32(rate * 2), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, dataLen,
	}
	for _, v := range hdr {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	if _, err := w.Write(ToBytes(pcm)); err != nil {
		return err
	}
	return w.Flush()
}

// WriteFileAtomic writes data through a temporary file and a rename, so a
// crash never leaves a half-written file at path.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
