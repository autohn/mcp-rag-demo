// Package extract turns an uploaded file into page texts.
//
// PDF yields one Page per PDF page (Paged=true). DOCX/TXT/MD yield conventional
// blocks whose numbers are not shown to users (Paged=false).
package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

type Page struct {
	Number int
	Text   string
}

type Result struct {
	Pages []Page
	Paged bool
}

// blockRunes is the size of a conventional "page" for unpaged formats.
const blockRunes = 6000

var ErrUnsupported = errors.New("unsupported format: PDF, DOCX, TXT or MD expected")

// Kind returns the canonical document kind by file name, or "" if unsupported.
func Kind(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".txt":
		return "txt"
	case ".md", ".markdown":
		return "md"
	}
	return ""
}

func Mime(kind string) string {
	switch kind {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "md":
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func File(path, kind string) (res Result, err error) {
	defer func() {
		// Malformed PDFs can panic inside the parser.
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to parse file: %v", r)
		}
	}()
	switch kind {
	case "pdf":
		return extractPDF(path)
	case "docx":
		text, err := extractDOCX(path)
		if err != nil {
			return Result{}, err
		}
		return Result{Pages: blocks(text)}, nil
	case "txt", "md":
		b, err := os.ReadFile(path)
		if err != nil {
			return Result{}, err
		}
		b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
		if !utf8.Valid(b) {
			return Result{}, errors.New("text file is not UTF-8 encoded")
		}
		return Result{Pages: blocks(string(b))}, nil
	}
	return Result{}, ErrUnsupported
}

func extractPDF(path string) (Result, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return Result{}, fmt.Errorf("failed to open PDF: %w", err)
	}
	defer f.Close()

	res := Result{Paged: true}
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		rows, err := p.GetTextByRow()
		if err != nil {
			return Result{}, fmt.Errorf("page %d: %w", i, err)
		}
		sort.SliceStable(rows, func(a, b int) bool { return rows[a].Position > rows[b].Position })
		var sb strings.Builder
		for _, row := range rows {
			line := joinRow(row.Content)
			if strings.TrimSpace(line) != "" {
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
		}
		res.Pages = append(res.Pages, Page{Number: i, Text: clean(sb.String())})
	}
	return res, nil
}

// joinRow glues text pieces of one line, inserting spaces where pieces are visibly apart.
func joinRow(items pdf.TextHorizontal) string {
	sort.SliceStable(items, func(a, b int) bool { return items[a].X < items[b].X })
	var sb strings.Builder
	prevEnd := -1.0
	for _, t := range items {
		if prevEnd >= 0 {
			gap := t.X - prevEnd
			if gap > t.FontSize*0.15 && !strings.HasPrefix(t.S, " ") && !strings.HasSuffix(sb.String(), " ") {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(t.S)
		prevEnd = t.X + t.W
	}
	return sb.String()
}

func extractDOCX(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("failed to open DOCX: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return docxText(rc)
	}
	return "", errors.New("DOCX has no word/document.xml")
}

func docxText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var sb strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				sb.WriteByte('\t')
			case "br", "cr":
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				sb.WriteByte('\n')
			case "tc":
				sb.WriteString(" | ")
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return clean(sb.String()), nil
}

// blocks splits long unpaged text into conventional pages on paragraph boundaries.
func blocks(text string) []Page {
	text = clean(text)
	if text == "" {
		return nil
	}
	var pages []Page
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			pages = append(pages, Page{Number: len(pages) + 1, Text: s})
		}
		cur.Reset()
	}
	for _, para := range strings.Split(text, "\n\n") {
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+utf8.RuneCountInString(para) > blockRunes {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return pages
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	s = strings.ReplaceAll(s, "\x00", "")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	s = strings.Join(lines, "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}
