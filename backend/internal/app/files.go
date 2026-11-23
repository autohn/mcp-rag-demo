package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcp-rag-demo/backend/internal/extract"
)

// ---- folders ----

func (a *App) folderProject(ctx context.Context, id int64) (int64, error) {
	var pid int64
	err := a.DB.QueryRow(ctx, `SELECT project_id FROM folders WHERE id=$1`, id).Scan(&pid)
	if isNoRows(err) {
		return 0, errf(KindNotFound, "folder %d not found", id)
	}
	return pid, err
}

// checkFolderIn verifies that folderID (if any) belongs to projectID.
func (a *App) checkFolderIn(ctx context.Context, projectID int64, folderID *int64) error {
	if folderID == nil {
		return nil
	}
	pid, err := a.folderProject(ctx, *folderID)
	if err != nil {
		return err
	}
	if pid != projectID {
		return errf(KindNotFound, "folder %d not found in this project", *folderID)
	}
	return nil
}

func scanFolders(rows pgx.Rows) ([]Folder, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Folder, error) {
		var f Folder
		err := r.Scan(&f.ID, &f.ProjectID, &f.ParentID, &f.Name)
		return f, err
	})
}

// AllFolders returns the whole folder tree of a project (flat).
func (a *App) AllFolders(ctx context.Context, uid, projectID int64) ([]Folder, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT id, project_id, parent_id, name FROM folders WHERE project_id=$1 ORDER BY lower(name)`, projectID)
	if err != nil {
		return nil, err
	}
	return scanFolders(rows)
}

type FolderListing struct {
	Folder    *Folder    `json:"folder"`
	Path      []Folder   `json:"path"`
	Folders   []Folder   `json:"folders"`
	Documents []Document `json:"documents"`
}

// ListFolder returns subfolders and documents of a folder (root when folderID is nil).
func (a *App) ListFolder(ctx context.Context, uid, projectID int64, folderID *int64) (FolderListing, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return FolderListing{}, err
	}
	if err := a.checkFolderIn(ctx, projectID, folderID); err != nil {
		return FolderListing{}, err
	}
	out := FolderListing{Path: []Folder{}}
	if folderID != nil {
		rows, err := a.DB.Query(ctx, `
			WITH RECURSIVE up AS (
				SELECT id, project_id, parent_id, name, 0 AS depth FROM folders WHERE id=$1
				UNION ALL
				SELECT f.id, f.project_id, f.parent_id, f.name, up.depth+1 FROM folders f JOIN up ON f.id = up.parent_id
			) SELECT id, project_id, parent_id, name FROM up ORDER BY depth DESC`, *folderID)
		if err != nil {
			return out, err
		}
		if out.Path, err = scanFolders(rows); err != nil {
			return out, err
		}
		out.Folder = &out.Path[len(out.Path)-1]
	}
	rows, err := a.DB.Query(ctx, `SELECT id, project_id, parent_id, name FROM folders
		WHERE project_id=$1 AND parent_id IS NOT DISTINCT FROM $2 ORDER BY lower(name)`, projectID, folderID)
	if err != nil {
		return out, err
	}
	if out.Folders, err = scanFolders(rows); err != nil {
		return out, err
	}
	out.Documents, err = a.queryDocuments(ctx, `d.project_id=$1 AND d.folder_id IS NOT DISTINCT FROM $2 ORDER BY lower(d.name)`, projectID, folderID)
	return out, err
}

func (a *App) CreateFolder(ctx context.Context, uid, projectID int64, parentID *int64, name string) (Folder, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleAdmin); err != nil {
		return Folder{}, err
	}
	name, err := cleanName(name, "folder")
	if err != nil {
		return Folder{}, err
	}
	if err := a.checkFolderIn(ctx, projectID, parentID); err != nil {
		return Folder{}, err
	}
	f := Folder{ProjectID: projectID, ParentID: parentID, Name: name}
	err = a.DB.QueryRow(ctx, `INSERT INTO folders(project_id, parent_id, name) VALUES ($1,$2,$3) RETURNING id`,
		projectID, parentID, name).Scan(&f.ID)
	return f, err
}

func (a *App) RenameFolder(ctx context.Context, uid, id int64, name string) (Folder, error) {
	pid, err := a.folderProject(ctx, id)
	if err != nil {
		return Folder{}, err
	}
	if _, err := a.authorize(ctx, a.DB, uid, pid, RoleAdmin); err != nil {
		return Folder{}, err
	}
	if name, err = cleanName(name, "folder"); err != nil {
		return Folder{}, err
	}
	var f Folder
	err = a.DB.QueryRow(ctx, `UPDATE folders SET name=$2 WHERE id=$1 RETURNING id, project_id, parent_id, name`, id, name).
		Scan(&f.ID, &f.ProjectID, &f.ParentID, &f.Name)
	return f, err
}

// MoveFolder moves a folder under a new parent (nil = root), refusing cycles.
func (a *App) MoveFolder(ctx context.Context, uid, id int64, target *int64) (Folder, error) {
	pid, err := a.folderProject(ctx, id)
	if err != nil {
		return Folder{}, err
	}
	if _, err := a.authorize(ctx, a.DB, uid, pid, RoleAdmin); err != nil {
		return Folder{}, err
	}
	if err := a.checkFolderIn(ctx, pid, target); err != nil {
		return Folder{}, err
	}
	if target != nil {
		var cycle bool
		err := a.DB.QueryRow(ctx, `
			WITH RECURSIVE sub AS (SELECT id FROM folders WHERE id=$1 UNION ALL SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id)
			SELECT EXISTS(SELECT 1 FROM sub WHERE id=$2)`, id, *target).Scan(&cycle)
		if err != nil {
			return Folder{}, err
		}
		if cycle {
			return Folder{}, errf(KindBadRequest, "cannot move a folder into itself")
		}
	}
	var f Folder
	err = a.DB.QueryRow(ctx, `UPDATE folders SET parent_id=$2 WHERE id=$1 RETURNING id, project_id, parent_id, name`, id, target).
		Scan(&f.ID, &f.ProjectID, &f.ParentID, &f.Name)
	return f, err
}

// DeleteFolder deletes a folder with all its subfolders and Documents.
func (a *App) DeleteFolder(ctx context.Context, uid, id int64) error {
	pid, err := a.folderProject(ctx, id)
	if err != nil {
		return err
	}
	if _, err := a.authorize(ctx, a.DB, uid, pid, RoleAdmin); err != nil {
		return err
	}
	rows, err := a.DB.Query(ctx, `
		WITH RECURSIVE sub AS (SELECT id FROM folders WHERE id=$1 UNION ALL SELECT f.id FROM folders f JOIN sub ON f.parent_id = sub.id)
		DELETE FROM documents WHERE folder_id IN (SELECT id FROM sub) RETURNING path`, id)
	if err != nil {
		return err
	}
	paths, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(ctx, `DELETE FROM folders WHERE id=$1`, id); err != nil {
		return err
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	return nil
}

// ---- documents ----

const docColumns = `d.id, d.project_id, d.folder_id, d.name, d.mime, d.size, d.path, d.status, d.error,
	d.page_count, d.chunk_count, d.paged, d.uploaded_by, COALESCE(u.name, ''), d.created_at`

func (a *App) queryDocuments(ctx context.Context, where string, args ...any) ([]Document, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+docColumns+` FROM documents d LEFT JOIN users u ON u.id = d.uploaded_by WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Document, error) {
		var d Document
		err := r.Scan(&d.ID, &d.ProjectID, &d.FolderID, &d.Name, &d.Mime, &d.Size, &d.path, &d.Status, &d.Error,
			&d.PageCount, &d.ChunkCount, &d.Paged, &d.UploadedBy, &d.UploadedByName, &d.CreatedAt)
		d.Kind = kindOfMime(d.Mime)
		d.NoText = d.Status == "ready" && d.ChunkCount == 0
		return d, err
	})
}

func kindOfMime(m string) string {
	for _, k := range []string{"pdf", "docx", "md", "txt"} {
		if extract.Mime(k) == m {
			return k
		}
	}
	return "txt"
}

func (a *App) loadDocument(ctx context.Context, id int64) (Document, error) {
	docs, err := a.queryDocuments(ctx, `d.id=$1`, id)
	if err != nil {
		return Document{}, err
	}
	if len(docs) == 0 {
		return Document{}, errf(KindNotFound, "document %d not found", id)
	}
	return docs[0], nil
}

// documentFor loads a document and authorizes the caller on its project.
func (a *App) documentFor(ctx context.Context, uid, id int64, need Role) (Document, error) {
	d, err := a.loadDocument(ctx, id)
	if err != nil {
		return Document{}, err
	}
	if _, err := a.authorize(ctx, a.DB, uid, d.ProjectID, need); err != nil {
		if ErrKind(err) == KindNotFound {
			return Document{}, errf(KindNotFound, "document %d not found", id)
		}
		return Document{}, err
	}
	return d, nil
}

func (a *App) GetDocument(ctx context.Context, uid, id int64) (Document, error) {
	return a.documentFor(ctx, uid, id, RoleParticipant)
}

// OpenDocumentFile returns the stored file for download.
func (a *App) OpenDocumentFile(ctx context.Context, uid, id int64) (Document, *os.File, error) {
	d, err := a.documentFor(ctx, uid, id, RoleParticipant)
	if err != nil {
		return Document{}, nil, err
	}
	f, err := os.Open(d.path)
	if err != nil {
		return Document{}, nil, errf(KindNotFound, "document file is missing on disk")
	}
	return d, f, nil
}

func (a *App) DocumentPages(ctx context.Context, uid, id int64) ([]Page, error) {
	if _, err := a.documentFor(ctx, uid, id, RoleParticipant); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT page, text FROM document_pages WHERE document_id=$1 ORDER BY page`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Page, error) {
		var p Page
		return p, r.Scan(&p.Page, &p.Text)
	})
}

// Upload stores a new Document and queues it for indexing. It returns the document in status processing.
func (a *App) Upload(ctx context.Context, uid, projectID int64, folderID *int64, filename string, r io.Reader) (Document, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleAdmin); err != nil {
		return Document{}, err
	}
	if err := a.checkFolderIn(ctx, projectID, folderID); err != nil {
		return Document{}, err
	}
	filename = strings.TrimSpace(filepath.Base(strings.ReplaceAll(filename, "\\", "/")))
	kind := extract.Kind(filename)
	if kind == "" {
		return Document{}, &Error{Kind: KindBadRequest, Msg: extract.ErrUnsupported.Error()}
	}

	dir := a.projectDir(projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Document{}, err
	}
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return Document{}, err
	}
	defer os.Remove(tmp.Name()) // no-op after successful rename
	limit := a.Cfg.MaxUploadMB << 20
	n, err := io.Copy(tmp, io.LimitReader(r, limit+1))
	tmp.Close()
	if err != nil {
		return Document{}, fmt.Errorf("write file: %w", err)
	}
	if n > limit {
		return Document{}, errf(KindTooLarge, "file is larger than %d MB", a.Cfg.MaxUploadMB)
	}
	if n == 0 {
		return Document{}, errf(KindBadRequest, "file is empty")
	}

	var id int64
	err = a.DB.QueryRow(ctx, `INSERT INTO documents(project_id, folder_id, name, mime, size, path, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,'',$6) RETURNING id`, projectID, folderID, filename, extract.Mime(kind), n, uid).Scan(&id)
	if err != nil {
		return Document{}, err
	}
	final := filepath.Join(dir, strconv.FormatInt(id, 10))
	if err := os.Rename(tmp.Name(), final); err != nil {
		a.DB.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
		return Document{}, err
	}
	if _, err := a.DB.Exec(ctx, `UPDATE documents SET path=$2 WHERE id=$1`, id, final); err != nil {
		return Document{}, err
	}
	a.Enqueue(id)
	return a.loadDocument(ctx, id)
}

func (a *App) RenameDocument(ctx context.Context, uid, id int64, name string) (Document, error) {
	if _, err := a.documentFor(ctx, uid, id, RoleAdmin); err != nil {
		return Document{}, err
	}
	name, err := cleanName(name, "document")
	if err != nil {
		return Document{}, err
	}
	if _, err := a.DB.Exec(ctx, `UPDATE documents SET name=$2 WHERE id=$1`, id, name); err != nil {
		return Document{}, err
	}
	return a.loadDocument(ctx, id)
}

func (a *App) MoveDocument(ctx context.Context, uid, id int64, target *int64) (Document, error) {
	d, err := a.documentFor(ctx, uid, id, RoleAdmin)
	if err != nil {
		return Document{}, err
	}
	if err := a.checkFolderIn(ctx, d.ProjectID, target); err != nil {
		return Document{}, err
	}
	if _, err := a.DB.Exec(ctx, `UPDATE documents SET folder_id=$2 WHERE id=$1`, id, target); err != nil {
		return Document{}, err
	}
	return a.loadDocument(ctx, id)
}

// DeleteDocument hard-deletes the file and its chunks; Citations keep their quotes.
func (a *App) DeleteDocument(ctx context.Context, uid, id int64) error {
	d, err := a.documentFor(ctx, uid, id, RoleAdmin)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id); err != nil {
		return err
	}
	if d.path != "" {
		if err := os.Remove(d.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logErr("remove file", err, "path", d.path)
		}
	}
	return nil
}
