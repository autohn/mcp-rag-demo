package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPDFs(t *testing.T) {
	files, _ := filepath.Glob("../seed/files/*.pdf")
	if len(files) == 0 {
		t.Skip("no seed PDFs")
	}
	for _, f := range files {
		res, err := File(f, "pdf")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !res.Paged || len(res.Pages) < 2 {
			t.Fatalf("%s: expected several pages, got %d", f, len(res.Pages))
		}
		all := ""
		for _, p := range res.Pages {
			all += p.Text + "\n"
		}
		if os.Getenv("SHOW") != "" {
			t.Logf("%s:\n%s", f, all)
		}
		for _, want := range []string{"Корпус", "Количество"} {
			if strings.Contains(f, "Спецификация") && !strings.Contains(all, want) {
				t.Errorf("%s: missing %q", f, want)
			}
		}
		wantPages := map[bool]int{true: 3, false: 2}[strings.Contains(f, "ПЗ")]
		if len(res.Pages) != wantPages {
			t.Errorf("%s: want %d pages, got %d", f, wantPages, len(res.Pages))
		}
		if strings.Contains(f, "ПЗ") && !strings.Contains(all, "бетона класса B30") {
			t.Errorf("%s: missing literal concrete grade", f)
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
