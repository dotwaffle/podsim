package view

import (
	"strings"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// wrapText splits value at spaces into lines that fit the width of fit.
// It returns at most limit lines, and no lines for a value with no words.
// When value needs more lines, the last line ends with an ellipsis. A word
// that does not fit on a line is cut.
func wrapText(value string, fit textFit, limit int) []string {
	words := strings.Fields(value)
	var lines []string
	for len(words) > 0 && len(lines) < limit {
		count := len(words)
		if len(lines) < limit-1 {
			count = lineWordCount(words, fit)
		}
		lines = append(lines, fitText(strings.Join(words[:count], " "), fit))
		words = words[count:]
	}
	return lines
}

// lineWordCount returns the number of words from the start of words that
// fit on one line of fit. The count is 1 or more, so a word that does not
// fit gets its own line.
func lineWordCount(words []string, fit textFit) int {
	count := 1
	for count < len(words) {
		if width, _ := text.Measure(strings.Join(words[:count+1], " "), fit.face, 0); width > fit.width {
			break
		}
		count++
	}
	return count
}
