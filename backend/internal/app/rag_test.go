package app

import "testing"

func TestFindQuoteNormalizes(t *testing.T) {
	text := "Колонны и стены с 1-го по 4-й этаж выполняются из бетона класса\nB30, с 5-го по 12-й\nэтаж — из бетона класса B25."
	got, ok := findQuote(text, "из бетона класса B30, с 5-го по 12-й этаж - из бетона класса B25")
	if !ok {
		t.Fatal("quote not found")
	}
	want := "из бетона класса\nB30, с 5-го по 12-й\nэтаж — из бетона класса B25"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, ok := findQuote(text, "из бетона класса B40"); ok {
		t.Fatal("fabricated quote accepted")
	}
	if got, ok := findQuote("Ёмкость «бака» 5 м³", `емкость "бака"`); !ok || got != "Ёмкость «бака»" {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestVerifyCitations(t *testing.T) {
	frags := []Fragment{
		{ChunkID: 10, DocumentID: 1, DocumentName: "ПЗ.pdf", Page: 2, Paged: true, Text: "Фундамент — плита толщиной 800 мм из бетона класса B30."},
		{ChunkID: 11, DocumentID: 2, DocumentName: "Спец.pdf", Page: 1, Paged: true, Text: "Поз. ОК-1 — окно 1500 × 1800 мм. Количество: 264 шт."},
	}
	ma := modelAnswer{Answer: "Плита 800 мм [1], бетон B40 [2], окон ОК-1 264 шт. [3][4]", Found: true}
	ma.Citations = append(ma.Citations,
		struct {
			Fragment int    `json:"fragment"`
			Quote    string `json:"quote"`
		}{1, "плита толщиной 800 мм"},
		struct {
			Fragment int    `json:"fragment"`
			Quote    string `json:"quote"`
		}{1, "бетона класса B40"}, // not in text: dropped
		struct {
			Fragment int    `json:"fragment"`
			Quote    string `json:"quote"`
		}{9, "Количество: 264 шт."}, // fragment not retrieved: dropped
		struct {
			Fragment int    `json:"fragment"`
			Quote    string `json:"quote"`
		}{2, "Количество: 264 шт."},
	)
	answer, cits := verifyCitations(ma, frags)
	if len(cits) != 2 {
		t.Fatalf("want 2 citations, got %d: %+v", len(cits), cits)
	}
	if answer != "Плита 800 мм [1], бетон B40, окон ОК-1 264 шт. [2]" {
		t.Fatalf("answer markers: %q", answer)
	}
	if cits[1].DocumentName != "Спец.pdf" || cits[1].N != 2 {
		t.Fatalf("bad citation %+v", cits[1])
	}
}

func TestFTSQuery(t *testing.T) {
	if got := ftsQuery("Сколько окон ОК-1 и бетона B7.5 в осях 3–7?"); got != "сколько | окон | ок-1 | бетона | b7.5 | осях | 3 | 7" {
		t.Fatalf("got %q", got)
	}
}
