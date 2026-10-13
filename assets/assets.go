// Package assets embeds the handwriting font used for all SVG text.
package assets

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font/sfnt"
)

// Caveat is a handwriting font by Impallari Type, SIL Open Font License 1.1
// (see OFL.txt). It covers Latin and Cyrillic.
//
//go:embed Caveat.ttf
var caveatTTF []byte

// Font returns the parsed embedded font. sfnt.Font is safe for concurrent
// use as long as every goroutine passes its own sfnt.Buffer.
var Font = sync.OnceValues(func() (*sfnt.Font, error) {
	return sfnt.Parse(caveatTTF)
})
