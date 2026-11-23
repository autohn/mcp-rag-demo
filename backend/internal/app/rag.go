package app

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

const (
	TopK           = 8
	NotFoundAnswer = "The project documents do not contain an answer to this question."
	rrfK           = 60
	candidatePool  = 40
)

// Hyphen/dot-joined tokens (ОК-1, B7.5, А-12) stay whole so Postgres matches them as phrases.
var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+(?:[-.][\p{L}\p{N}]+)*`)

// ftsQuery turns a natural-language question into an OR tsquery so that literal tokens
// (axis numbers, concrete grades, item codes) match even when other words do not.
func ftsQuery(q string) string {
	seen := map[string]bool{}
	var terms []string
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if seen[w] || (utf8.RuneCountInString(w) < 2 && !unicode.IsDigit([]rune(w)[0])) {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
		if len(terms) == 30 {
			break
		}
	}
	return strings.Join(terms, " | ")
}

// Search runs hybrid retrieval (pgvector cosine + Postgres full-text, fused with RRF),
// always restricted to one project and optionally to a folder subtree.
func (a *App) Search(ctx context.Context, uid, projectID int64, query string, folderID *int64, limit int) ([]Fragment, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return nil, err
	}
	if err := a.checkFolderIn(ctx, projectID, folderID); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errf(KindBadRequest, "empty query")
	}
	if limit <= 0 || limit > 50 {
		limit = TopK
	}
	return a.search(ctx, projectID, query, folderID, limit)
}

func (a *App) search(ctx context.Context, projectID int64, query string, folderID *int64, limit int) ([]Fragment, error) {
	var vec *pgvector.Vector
	if a.LLM.Enabled() {
		vs, err := a.LLM.Embed(ctx, []string{query})
		if err != nil {
			return nil, err
		}
		v := pgvector.NewVector(vs[0])
		vec = &v
	}
	rows, err := a.DB.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM folders WHERE id = $2
			UNION ALL SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id
		),
		docs AS (
			SELECT id FROM documents
			WHERE project_id = $1 AND ($2::bigint IS NULL OR folder_id IN (SELECT id FROM sub))
		),
		vec AS (
			SELECT c.id, row_number() OVER (ORDER BY c.embedding <=> $3::vector) AS r
			FROM chunks c
			WHERE $3::vector IS NOT NULL AND c.project_id = $1 AND c.embedding IS NOT NULL
			  AND c.document_id IN (SELECT id FROM docs)
			ORDER BY c.embedding <=> $3::vector LIMIT $6
		),
		fts AS (
			SELECT c.id, row_number() OVER (ORDER BY ts_rank_cd(c.tsv, q.q) DESC) AS r
			FROM chunks c, (SELECT to_tsquery('russian', $4) AS q) q
			WHERE $4 <> '' AND c.project_id = $1 AND c.tsv @@ q.q
			  AND c.document_id IN (SELECT id FROM docs)
			ORDER BY ts_rank_cd(c.tsv, q.q) DESC LIMIT $6
		),
		fused AS (
			SELECT id, sum(1.0 / ($7 + r)) AS score
			FROM (SELECT id, r FROM vec UNION ALL SELECT id, r FROM fts) x GROUP BY id
		)
		SELECT c.id, c.document_id, d.name, c.page, d.paged, c.text, f.score::float8
		FROM fused f JOIN chunks c ON c.id = f.id JOIN documents d ON d.id = c.document_id
		ORDER BY f.score DESC, c.id LIMIT $5`,
		projectID, folderID, vec, ftsQuery(query), limit, candidatePool, rrfK)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Fragment, error) {
		var f Fragment
		return f, r.Scan(&f.ChunkID, &f.DocumentID, &f.DocumentName, &f.Page, &f.Paged, &f.Text, &f.Score)
	})
}

// ---- answering ----

const systemPrompt = `You are an assistant for a construction project's documentation. Answer the question ONLY from the provided document fragments. Do not use outside knowledge and do not guess.

Rules:
- Back every claim in the answer with a citation marker [n], where n is the 1-based index of the citation in the citations array. Write several markers back to back: [1][2].
- Each citation: fragment is the fragment number, quote is a verbatim contiguous passage from that fragment's text. Copy it character for character, in the original language, with no ellipses, abbreviations or paraphrasing; usually one or two sentences or one table row.
- If the fragments do not answer the question, return found=false, an empty citations array and answer "Not found in the project documents".
- Answer in the language of the question, briefly and to the point. Keep exact values: axis numbers, concrete grades, item codes, dimensions and units.`

var answerSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"answer", "found", "citations"},
	"properties": map[string]any{
		"answer": map[string]any{"type": "string"},
		"found":  map[string]any{"type": "boolean"},
		"citations": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"fragment", "quote"},
				"properties": map[string]any{
					"fragment": map[string]any{"type": "integer"},
					"quote":    map[string]any{"type": "string"},
				},
			},
		},
	},
}

type modelAnswer struct {
	Answer    string `json:"answer"`
	Found     bool   `json:"found"`
	Citations []struct {
		Fragment int    `json:"fragment"`
		Quote    string `json:"quote"`
	} `json:"citations"`
}

// Ask answers a Question from the project's Documents and stores it in the caller's history.
func (a *App) Ask(ctx context.Context, uid, projectID int64, question string, folderID *int64) (QuestionRecord, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return QuestionRecord{}, err
	}
	if err := a.checkFolderIn(ctx, projectID, folderID); err != nil {
		return QuestionRecord{}, err
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return QuestionRecord{}, errf(KindBadRequest, "question is empty")
	}
	if !a.LLM.Enabled() {
		return QuestionRecord{}, errf(KindUnavailable, "Answers are unavailable: OPENAI_API_KEY is not set on the server. Fragment search (search_project) still works.")
	}

	frags, err := a.search(ctx, projectID, question, folderID, TopK)
	if err != nil {
		return QuestionRecord{}, err
	}
	rec := QuestionRecord{ProjectID: projectID, FolderID: folderID, Question: question, Answer: NotFoundAnswer, Citations: []Citation{}}
	if len(frags) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Question: %s\n\nFragments:\n", question)
		for i, f := range frags {
			loc := ""
			if f.Paged {
				loc = fmt.Sprintf(", p. %d", f.Page)
			}
			fmt.Fprintf(&sb, "\n[Fragment %d] Document \"%s\"%s\n%s\n", i+1, f.DocumentName, loc, f.Text)
		}
		var ma modelAnswer
		if err := a.LLM.ChatJSON(ctx, systemPrompt, sb.String(), "cited_answer", answerSchema, &ma); err != nil {
			return QuestionRecord{}, err
		}
		if ma.Found {
			rec.Answer, rec.Citations = verifyCitations(ma, frags)
			rec.Found = len(rec.Citations) > 0
			if !rec.Found {
				rec.Answer = NotFoundAnswer
			}
		}
	}
	return rec, a.saveQuestion(ctx, uid, &rec)
}

var markerRe = regexp.MustCompile(`\[(\d+(?:\s*,\s*\d+)*)\]`)

// verifyCitations drops citations whose fragment was not retrieved or whose quote is not in
// the fragment text, replaces quotes with the exact source text, and renumbers [n] markers.
func verifyCitations(ma modelAnswer, frags []Fragment) (string, []Citation) {
	renum := map[int]int{}
	seen := map[string]int{}
	var cits []Citation
	for i, c := range ma.Citations {
		if c.Fragment < 1 || c.Fragment > len(frags) {
			continue
		}
		f := frags[c.Fragment-1]
		exact, ok := findQuote(f.Text, c.Quote)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d|%s", f.ChunkID, exact)
		if n, dup := seen[key]; dup {
			renum[i+1] = n
			continue
		}
		docID := f.DocumentID
		cits = append(cits, Citation{N: len(cits) + 1, DocumentID: &docID, DocumentName: f.DocumentName, Page: f.Page, Paged: f.Paged, Quote: exact})
		seen[key] = len(cits)
		renum[i+1] = len(cits)
	}
	answer := markerRe.ReplaceAllStringFunc(ma.Answer, func(m string) string {
		var out strings.Builder
		for _, part := range strings.Split(strings.Trim(m, "[]"), ",") {
			n, _ := strconv.Atoi(strings.TrimSpace(part))
			if nn, ok := renum[n]; ok {
				fmt.Fprintf(&out, "[%d]", nn)
			}
		}
		return out.String()
	})
	answer = spaceBeforePunctRe.ReplaceAllString(answer, "$1")
	return strings.TrimSpace(answer), cits
}

var spaceBeforePunctRe = regexp.MustCompile(`[ \t]+([.,;:!?])`)

// normalize folds case, ё, quotes, dashes and whitespace; idx maps each output rune to its byte offset in s.
func normalize(s string) (out []rune, idx []int) {
	prevSpace := true
	for i, r := range s {
		switch {
		case unicode.IsSpace(r) || r == '­':
			if r == '­' {
				continue
			}
			if prevSpace {
				continue
			}
			r = ' '
			prevSpace = true
		default:
			prevSpace = false
			r = unicode.ToLower(r)
			switch r {
			case 'ё':
				r = 'е'
			case '«', '»', '“', '”', '„', '″':
				r = '"'
			case '‘', '’', '′':
				r = '\''
			case '–', '—', '‑', '−':
				r = '-'
			}
		}
		out = append(out, r)
		idx = append(idx, i)
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out, idx = out[:len(out)-1], idx[:len(idx)-1]
	}
	return out, idx
}

// findQuote locates quote inside text with normalized comparison and returns the exact source span.
func findQuote(text, quote string) (string, bool) {
	quote = strings.Trim(strings.TrimSpace(quote), "…")
	if r := []rune(quote); len(r) > 2 && strings.ContainsRune(`"«“`, r[0]) && strings.ContainsRune(`"»”`, r[len(r)-1]) {
		quote = string(r[1 : len(r)-1]) // the model wrapped the whole quote in quotation marks
	}
	q, _ := normalize(strings.TrimSpace(quote))
	if len(q) < 3 {
		return "", false
	}
	t, idx := normalize(text)
	pos := indexRunes(t, q)
	if pos < 0 {
		return "", false
	}
	start := idx[pos]
	last := idx[pos+len(q)-1]
	_, size := utf8.DecodeRuneInString(text[last:])
	return text[start : last+size], true
}

func indexRunes(h, n []rune) int {
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := range n {
			if h[i+j] != n[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

func (a *App) saveQuestion(ctx context.Context, uid int64, rec *QuestionRecord) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO questions(project_id, user_id, folder_id, text, answer, found)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		rec.ProjectID, uid, rec.FolderID, rec.Question, rec.Answer, rec.Found).Scan(&rec.ID, &rec.CreatedAt)
	if err != nil {
		return err
	}
	for _, c := range rec.Citations {
		// The document may have been deleted since retrieval; keep the quote, drop the link.
		if _, err := tx.Exec(ctx, `INSERT INTO citations(question_id, ord, document_id, document_name, page, paged, quote)
			VALUES ($1,$2,(SELECT id FROM documents WHERE id=$3),$4,$5,$6,$7)`,
			rec.ID, c.N, c.DocumentID, c.DocumentName, c.Page, c.Paged, c.Quote); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// History returns the caller's own Questions in a project, newest first.
func (a *App) History(ctx context.Context, uid, projectID int64, limit int) ([]QuestionRecord, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := a.DB.Query(ctx, `SELECT id, project_id, folder_id, text, answer, found, created_at FROM questions
		WHERE project_id=$1 AND user_id=$2 ORDER BY created_at DESC LIMIT $3`, projectID, uid, limit)
	if err != nil {
		return nil, err
	}
	recs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (QuestionRecord, error) {
		q := QuestionRecord{Citations: []Citation{}}
		return q, r.Scan(&q.ID, &q.ProjectID, &q.FolderID, &q.Question, &q.Answer, &q.Found, &q.CreatedAt)
	})
	if err != nil || len(recs) == 0 {
		return recs, err
	}
	ids := make([]int64, len(recs))
	pos := map[int64]int{}
	for i, r := range recs {
		ids[i] = r.ID
		pos[r.ID] = i
	}
	crows, err := a.DB.Query(ctx, `SELECT question_id, ord, document_id, document_name, page, paged, quote FROM citations
		WHERE question_id = ANY($1) ORDER BY question_id, ord`, ids)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var qid int64
		var c Citation
		if err := crows.Scan(&qid, &c.N, &c.DocumentID, &c.DocumentName, &c.Page, &c.Paged, &c.Quote); err != nil {
			return nil, err
		}
		c.DocumentDeleted = c.DocumentID == nil
		recs[pos[qid]].Citations = append(recs[pos[qid]].Citations, c)
	}
	return recs, crows.Err()
}
