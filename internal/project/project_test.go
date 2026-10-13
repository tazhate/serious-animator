package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUnspokenWords(t *testing.T) {
	cases := []struct {
		label, narration string
		missing          []string
	}{
		{"СОЛНЦЕ", "Солнца светит ярко.", nil},
		{"ВОДА", "Корни пьют воды из почвы.", nil},
		{"CO2", "Plants grow because of light.", []string{"co2"}},
		{"CO2", "The leaf takes in CO2 from the air.", nil},
		{"PLANT", "Plants make food.", nil},
		{"КОТ", "Который час?", []string{"кот"}},
		{"Ёж и ЛИСА", "Еж встретил лису.", nil},
		{"IP-АДРЕС", "Компьютеры знают только IP-адреса.", nil},
		{"ЗОНА COM", "Сервер зоны com.", nil},
		{"КОРЕНЬ", "Резолвер идет к корню.", nil},
		{"ХЛОРОФИЛЛ", "Лист зеленый.", []string{"хлорофилл"}},
	}
	for _, c := range cases {
		got := UnspokenWords(c.label, c.narration)
		if !slices.Equal(got, c.missing) {
			t.Errorf("UnspokenWords(%q, %q) = %q, want %q", c.label, c.narration, got, c.missing)
		}
	}
}

const goodSVG = `<svg viewBox="0 0 1600 900">
  <g data-beat="1"><text x="100" y="100" font-size="50">СОЛНЦЕ</text><circle cx="300" cy="400" r="80" fill="#fd0" stroke="#222" stroke-width="5"/></g>
  <g data-beat="2"><text x="900" y="600" font-size="50">ВОДА</text><rect x="900" y="300" width="100" height="100" fill="#09f"/></g>
</svg>`

func writeProject(t *testing.T, board, svg string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, File), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "s01.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const goodBoard = `{"title":"Тест","lang":"ru","scenes":[{"id":"s01","title":"Т","svg":"scenes/s01.svg","beats":[
  {"label":"СОЛНЦЕ","narration":"Солнце дает свет."},
  {"label":"ВОДА","narration":"Вода поднимается по корням."}]}]}`

func TestLoadGoodProject(t *testing.T) {
	dir := writeProject(t, goodBoard, goodSVG)
	p, probs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
	if p.Board.Voice != "ru-RU-SvetlanaNeural" || p.Board.Rate != "+0%" {
		t.Errorf("defaults not applied: %q %q", p.Board.Voice, p.Board.Rate)
	}
	if p.Drawings[0] == nil || len(p.Drawings[0].Items) != 4 {
		t.Fatalf("drawing not loaded")
	}
}

func messages(ps []Problem) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestLoadReportsProblems(t *testing.T) {
	board := `{"title":"","voice":"bad voice","rate":"fast","scenes":[{"id":"S 1","title":"T","svg":"../escape.svg","beats":[
  {"label":"СОЛНЦЕ","narration":""}]}]}`
	dir := writeProject(t, board, goodSVG)
	_, probs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := messages(probs)
	for _, want := range []string{"title is empty", "voice", "rate", "id must match", "inside the project", "1 beats", "narration has 0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	if !HasErrors(probs) {
		t.Error("HasErrors = false")
	}
}

func TestBeatCoverage(t *testing.T) {
	svg := `<svg viewBox="0 0 1600 900">
  <circle cx="300" cy="400" r="80" fill="#fd0"/>
  <g data-beat="1"><text x="100" y="100" font-size="50">СОЛНЦЕ</text></g>
  <rect data-beat="5" x="900" y="300" width="100" height="100" fill="#09f"/>
</svg>`
	dir := writeProject(t, goodBoard, svg)
	_, probs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := messages(probs)
	for _, want := range []string{"has no data-beat", "data-beat=5", "beat 2 (\"ВОДА\") has nothing to draw"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	dir := writeProject(t, `{"title":"x","scenes":[],"sceens":[]}`, goodSVG)
	if _, _, err := Load(dir); err == nil {
		t.Fatal("typo in a field name should fail")
	}
}
