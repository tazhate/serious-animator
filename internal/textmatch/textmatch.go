// Package textmatch compares spoken words with caption words, allowing for
// inflection, so "корень" in a label matches "корню" in the narration.
package textmatch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Words lowercases s, folds ё into е and splits it into letter/number runs.
func Words(s string) []string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// SameWord matches two lowercase words allowing for inflection: "солнце" and
// "солнца", "plant" and "plants". Short words must match exactly or differ by
// one trailing letter, so "co2" does not match "because" and "кот" does not
// match "который".
func SameWord(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	m := min(len(ra), len(rb))
	p := 0
	for p < m && ra[p] == rb[p] {
		p++
	}
	if m < 4 {
		return p == m && max(len(ra), len(rb))-m <= 1
	}
	return p >= max(3, m-2)
}

// Checked reports whether a label word is worth matching: words of one or
// two letters ("и", "of") are skipped unless they hold a digit.
func Checked(w string) bool {
	return utf8.RuneCountInString(w) > 2 || strings.ContainsFunc(w, unicode.IsNumber)
}

// FirstLabelWord returns the index in spoken of the first word that matches
// any checked word of label, or -1.
func FirstLabelWord(label string, spoken []string) int {
	var lw []string
	for _, w := range Words(label) {
		if Checked(w) {
			lw = append(lw, w)
		}
	}
	for i, s := range spoken {
		for _, w := range Words(s) {
			for _, l := range lw {
				if SameWord(l, w) {
					return i
				}
			}
		}
	}
	return -1
}
