package project

import "github.com/tazhate/serious-animator/internal/textmatch"

// UnspokenWords returns the label words that the narration never says.
// Words of one or two letters ("и", "of") are not checked.
func UnspokenWords(label, narration string) []string {
	said := textmatch.Words(narration)
	var missing []string
	for _, lw := range textmatch.Words(label) {
		if !textmatch.Checked(lw) {
			continue
		}
		found := false
		for _, nw := range said {
			if textmatch.SameWord(lw, nw) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, lw)
		}
	}
	return missing
}

func normalize(s string) string {
	return joinWords(textmatch.Words(s))
}
