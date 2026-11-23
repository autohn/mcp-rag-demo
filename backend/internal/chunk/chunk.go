// Package chunk splits page text into overlapping chunks that never cross a page boundary.
package chunk

import (
	"strings"
	"unicode/utf8"
)

// Token sizes are approximated by runes: Russian text averages ~3.5 runes per token.
const (
	runesPerToken = 3.5
	TargetTokens  = 800
	OverlapTokens = 100
)

var (
	maxRunes     = int(TargetTokens * runesPerToken)
	overlapRunes = int(OverlapTokens * runesPerToken)
)

// Split returns the chunks of one page's text.
func Split(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}

	units := splitUnits(text)
	var chunks []string
	var cur []string
	curLen := 0
	for _, u := range units {
		ul := utf8.RuneCountInString(u)
		if curLen > 0 && curLen+ul > maxRunes {
			chunks = append(chunks, strings.TrimSpace(strings.Join(cur, "")))
			// Carry trailing units into the next chunk as overlap.
			var keep []string
			kept := 0
			for i := len(cur) - 1; i >= 0; i-- {
				l := utf8.RuneCountInString(cur[i])
				if kept+l > overlapRunes {
					break
				}
				keep = append([]string{cur[i]}, keep...)
				kept += l
			}
			cur, curLen = keep, kept
		}
		cur = append(cur, u)
		curLen += ul
	}
	if curLen > 0 {
		chunks = append(chunks, strings.TrimSpace(strings.Join(cur, "")))
	}
	return chunks
}

// splitUnits cuts text into sentence/line units (keeping separators), hard-splitting oversized ones.
func splitUnits(text string) []string {
	var units []string
	var b strings.Builder
	runes := []rune(text)
	n := 0
	for i, r := range runes {
		b.WriteRune(r)
		n++
		end := r == '\n' ||
			((r == '.' || r == '!' || r == '?' || r == ';') && i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n'))
		if end || n >= maxRunes/2 {
			units = append(units, b.String())
			b.Reset()
			n = 0
		}
	}
	if b.Len() > 0 {
		units = append(units, b.String())
	}
	return units
}
