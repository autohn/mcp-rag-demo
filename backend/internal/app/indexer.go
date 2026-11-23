package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"

	"mcp-rag-demo/backend/internal/chunk"
	"mcp-rag-demo/backend/internal/extract"
)

func (a *App) Enqueue(id int64) {
	select {
	case a.queue <- id:
	default:
		// Queue full: the document stays in processing and is picked up on next start.
		slog.Warn("index queue full", "document", id)
	}
}

// RunIndexer processes queued documents until ctx is done. Documents left in
// processing by a previous run are re-queued first.
func (a *App) RunIndexer(ctx context.Context, workers int) {
	rows, err := a.DB.Query(ctx, `SELECT id FROM documents WHERE status='processing' ORDER BY id`)
	if err == nil {
		ids, _ := pgx.CollectRows(rows, pgx.RowTo[int64])
		for _, id := range ids {
			a.Enqueue(id)
		}
	}
	for range workers {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-a.queue:
					a.index(ctx, id)
				}
			}
		}()
	}
}

func (a *App) index(ctx context.Context, id int64) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	start := time.Now()
	pages, chunks, err := a.indexDocument(ctx, id)
	if errors.Is(err, errDocumentGone) {
		return
	}
	if err != nil {
		slog.Warn("indexing failed", "document", id, "err", err)
		a.DB.Exec(ctx, `UPDATE documents SET status='failed', error=$2 WHERE id=$1`, id, err.Error())
		return
	}
	slog.Info("indexed", "document", id, "pages", pages, "chunks", chunks, "took", time.Since(start).Round(time.Millisecond))
}

var errDocumentGone = errors.New("document deleted")

type pendingChunk struct {
	page, ord int
	text      string
}

func (a *App) indexDocument(ctx context.Context, id int64) (int, int, error) {
	d, err := a.loadDocument(ctx, id)
	if ErrKind(err) == KindNotFound {
		return 0, 0, errDocumentGone
	}
	if err != nil {
		return 0, 0, err
	}
	res, err := extract.File(d.path, d.Kind)
	if err != nil {
		return 0, 0, err
	}

	var pending []pendingChunk
	for _, p := range res.Pages {
		for i, c := range chunk.Split(p.Text) {
			pending = append(pending, pendingChunk{page: p.Number, ord: i, text: c})
		}
	}

	// Without an API key the demo still works with full-text search only.
	var vectors [][]float32
	if a.LLM.Enabled() && len(pending) > 0 {
		texts := make([]string, len(pending))
		for i, c := range pending {
			texts[i] = c.text
		}
		if vectors, err = a.LLM.Embed(ctx, texts); err != nil {
			return 0, 0, err
		}
	}

	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	// Lock the row; if the document was deleted meanwhile, stop.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM documents WHERE id=$1 FOR UPDATE`, id).Scan(&exists); err != nil {
		if isNoRows(err) {
			return 0, 0, errDocumentGone
		}
		return 0, 0, err
	}
	tx.Exec(ctx, `DELETE FROM document_pages WHERE document_id=$1`, id)
	tx.Exec(ctx, `DELETE FROM chunks WHERE document_id=$1`, id)

	b := &pgx.Batch{}
	for _, p := range res.Pages {
		b.Queue(`INSERT INTO document_pages(document_id, page, text) VALUES ($1,$2,$3)`, id, p.Number, p.Text)
	}
	for i, c := range pending {
		var vec *pgvector.Vector
		if vectors != nil {
			v := pgvector.NewVector(vectors[i])
			vec = &v
		}
		b.Queue(`INSERT INTO chunks(document_id, project_id, page, ord, text, embedding, tsv)
			VALUES ($1,$2,$3,$4,$5,$6, to_tsvector('russian', $5))`, id, d.ProjectID, c.page, c.ord, c.text, vec)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET status='ready', error='', page_count=$2, chunk_count=$3, paged=$4 WHERE id=$1`,
		id, len(res.Pages), len(pending), res.Paged); err != nil {
		return 0, 0, err
	}
	return len(res.Pages), len(pending), tx.Commit(ctx)
}
