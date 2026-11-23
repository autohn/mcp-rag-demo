package chunk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitOverlapAndSize(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString("Плита перекрытия выполняется из бетона класса B25 толщиной 220 мм. ")
	}
	chunks := Split(sb.String())
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > maxRunes+100 {
			t.Fatalf("chunk %d too long: %d", i, n)
		}
	}
	// Overlap: the start of chunk 2 must appear at the end of chunk 1.
	head := []rune(chunks[1])[:60]
	if !strings.Contains(chunks[0], string(head)) {
		t.Fatal("no overlap between consecutive chunks")
	}
}

func TestSplitShort(t *testing.T) {
	if got := Split("  короткий текст "); len(got) != 1 || got[0] != "короткий текст" {
		t.Fatalf("got %q", got)
	}
	if Split("   ") != nil {
		t.Fatal("empty text should give no chunks")
	}
}
