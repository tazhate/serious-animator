package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrimSilence(t *testing.T) {
	pcm := make([]int16, 1000)
	pcm[300], pcm[600] = 5000, -5000
	got := TrimSilence(pcm, 10)
	if len(got) != 600-300+1+20 {
		t.Fatalf("len %d", len(got))
	}
	if TrimSilence(make([]int16, 50), 10) != nil {
		t.Fatal("pure silence should trim to nothing")
	}
}

func TestWAVHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	if err := WriteWAV(path, []int16{1, -1, 300}, Rate); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 44+6 || string(b[0:4]) != "RIFF" || string(b[36:40]) != "data" {
		t.Fatalf("bad header: % x", b[:44])
	}
	if binary.LittleEndian.Uint32(b[24:]) != Rate || binary.LittleEndian.Uint32(b[40:]) != 6 {
		t.Fatal("bad rate or data size")
	}
	if got := FromBytes(b[44:]); got[1] != -1 || got[2] != 300 {
		t.Fatalf("samples %v", got)
	}
}

func TestMixGraph(t *testing.T) {
	in, g := Mix{Voice: "v.wav", SFX: "fx.wav", Music: "m.mp3", LoopMusic: true, MusicDB: -20, Duration: 30}.MixGraph()
	if strings.Join(in, " ") != "-i v.wav -i fx.wav -stream_loop -1 -i m.mp3" {
		t.Fatalf("inputs %q", in)
	}
	for _, want := range []string{"[2:a]", "volume=-20.0dB", "sidechaincompress", "[m0][vk]", "[v][fx][m]amix=inputs=3", "afade=t=out:st=27.500"} {
		if !strings.Contains(g, want) {
			t.Errorf("graph lacks %q:\n%s", want, g)
		}
	}
	_, g = Mix{Voice: "v.wav", Duration: 5}.MixGraph()
	if !strings.Contains(g, "[vk]anullsink") || !strings.Contains(g, "[v]amix=inputs=1") {
		t.Errorf("voice-only graph: %s", g)
	}
}
