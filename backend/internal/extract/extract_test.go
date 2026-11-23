package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPDFs(t *testing.T) {
	cases := []struct {
		file  string
		pages int
		first string   // expected first line: catches bottom-up row order
		want  []string // literal passages that must survive extraction
	}{
		{"ПЗ_Конструктивные_решения.pdf", 3, "Жилой дом, корпус 2. Пояснительная записка.", []string{"бетона класса B30", "А500С"}},
		{"Спецификация_окон_и_дверей.pdf", 2, "Корпус 2. Раздел АР. Спецификация элементов", []string{"Поз. ОК-1", "Количество: 264"}},
		// Chromium/Skia PDF with a flipped Y axis.
		{"Mikhail_Aleksanian_CV.pdf", 1, "Mikhail Aleksanian", []string{"Experience", "Core Skills"}},
	}
	for _, c := range cases {
		path := filepath.Join("..", "seed", "files", c.file)
		res, err := File(path, "pdf")
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if !res.Paged || len(res.Pages) != c.pages {
			t.Fatalf("%s: want %d pages, got %d", c.file, c.pages, len(res.Pages))
		}
		all := ""
		for _, p := range res.Pages {
			all += p.Text + "\n"
		}
		if os.Getenv("SHOW") != "" {
			t.Logf("%s:\n%s", c.file, all)
		}
		if first := strings.SplitN(all, "\n", 2)[0]; first != c.first {
			t.Errorf("%s: first line %q, want %q", c.file, first, c.first)
		}
		for _, w := range c.want {
			if !strings.Contains(all, w) {
				t.Errorf("%s: missing %q", c.file, w)
			}
		}
	}
}

func TestDocxText(t *testing.T) {
	xml := `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>Привет</w:t></w:r><w:r><w:tab/><w:t>мир</w:t></w:r></w:p><w:p><w:r><w:t>Вторая</w:t></w:r></w:p></w:body></w:document>`
	got, err := docxText(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Привет\tмир\nВторая" {
		t.Fatalf("got %q", got)
	}
}
