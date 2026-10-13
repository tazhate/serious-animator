package textmatch

import "testing"

func TestFirstLabelWord(t *testing.T) {
	spoken := []string{"Мешалка", "вертелась", "в", "сосуде", "с", "водой,", "и", "вода"}
	if got := FirstLabelWord("ВОДА", spoken); got != 5 {
		t.Fatalf("got %d, want 5 (водой)", got)
	}
	if got := FirstLabelWord("ВНЕШНЕЕ КОЛЕСО", []string{"А", "колеса", "внешнего"}); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	if got := FirstLabelWord("ГРУЗ", spoken); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
}
