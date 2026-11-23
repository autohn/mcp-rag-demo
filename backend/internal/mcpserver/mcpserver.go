// Package mcpserver exposes the app as MCP tools over Streamable HTTP at /mcp.
// Each request is authenticated with a personal Access Token and served by a
// server bound to that User, so tools have exactly the User's rights.
package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-rag-demo/backend/internal/app"
)

type ctxKey struct{}

// Handler returns the /mcp HTTP handler.
func Handler(a *app.App) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		u, ok := r.Context().Value(ctxKey{}).(app.User)
		if !ok {
			return nil
		}
		return newServer(a, u)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 80 << 20})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(tok) == "" {
			unauthorized(w, "Authorization: Bearer <Access Token> header required")
			return
		}
		u, err := a.UserByAccessToken(r.Context(), strings.TrimSpace(tok))
		if err != nil {
			unauthorized(w, err.Error())
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

const instructions = `Construction documentation server. The user is a member of one or more Projects (construction sites).
Call list_projects first to get a project_id. For questions about documents use ask_project (a ready Answer with verified Citations: document, page, verbatim passage) or search_project (raw fragments for your own analysis). Answer only from the documents and always cite the source. Questions are independent: each one must be self-contained.`

func newServer(a *app.App, u app.User) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "construction-docs", Title: "Construction documentation", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: instructions})
	t := tools{a: a, uid: u.ID}

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	destructive := &mcp.ToolAnnotations{DestructiveHint: ptr(true)}

	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Projects of the user with their role (admin/participant).", Annotations: readOnly}, t.listProjects)
	mcp.AddTool(s, &mcp.Tool{Name: "create_project", Description: "Create a project. The caller becomes its Admin."}, t.createProject)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_project", Description: "Rename a project (Admin only)."}, t.renameProject)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_project", Description: "Delete a project with all its documents and history (Admin only). Irreversible.", Annotations: destructive}, t.deleteProject)

	mcp.AddTool(s, &mcp.Tool{Name: "list_members", Description: "Project members and their roles.", Annotations: readOnly}, t.listMembers)
	mcp.AddTool(s, &mcp.Tool{Name: "add_member", Description: "Add an already registered user to the project by email (Admin only)."}, t.addMember)
	mcp.AddTool(s, &mcp.Tool{Name: "remove_member", Description: "Remove a member from the project (Admin only). The last Admin cannot be removed.", Annotations: destructive}, t.removeMember)
	mcp.AddTool(s, &mcp.Tool{Name: "set_member_role", Description: "Change a member's role (Admin only). The last Admin cannot be demoted."}, t.setMemberRole)

	mcp.AddTool(s, &mcp.Tool{Name: "list_folder", Description: "Contents of a project folder (root by default): subfolders and documents with indexing status (processing/ready/failed).", Annotations: readOnly}, t.listFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "create_folder", Description: "Create a folder (Admin only)."}, t.createFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_folder", Description: "Rename a folder (Admin only)."}, t.renameFolder)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_document", Description: "Rename a document (Admin only)."}, t.renameDocument)
	mcp.AddTool(s, &mcp.Tool{Name: "move_item", Description: "Move a folder or document to another folder of the same project (Admin only). Without target_folder_id it goes to the root."}, t.moveItem)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_item", Description: "Delete a document, or a folder with all its contents (Admin only). Irreversible.", Annotations: destructive}, t.deleteItem)

	mcp.AddTool(s, &mcp.Tool{Name: "upload_document", Description: "Upload a document (PDF with a text layer, DOCX, TXT, MD; up to 50 MB) as either content_base64 or url (the server downloads it). Admin only. Returns the document in status processing; poll get_document for the status."}, t.uploadDocument)
	mcp.AddTool(s, &mcp.Tool{Name: "get_document", Description: "Document metadata and indexing status; optionally the extracted text of its pages.", Annotations: readOnly}, t.getDocument)

	mcp.AddTool(s, &mcp.Tool{Name: "ask_project", Description: "Ask a Question about the project documents. Returns an Answer where every claim is marked [n], plus verified Citations (document, page, verbatim passage). If the documents do not contain the answer, found=false. Saved to the user's personal history."}, t.askProject)
	mcp.AddTool(s, &mcp.Tool{Name: "search_project", Description: "Hybrid search (vector + full-text) over the project documents without answer generation: top-N fragments with document and page.", Annotations: readOnly}, t.searchProject)
	return s
}

func ptr[T any](v T) *T { return &v }

type tools struct {
	a   *app.App
	uid int64
}

// result wraps a value so every tool returns a JSON object.
type result[T any] struct {
	Result T `json:"result"`
}

func ok[T any](v T, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, toolErr(err)
	}
	return nil, result[T]{v}, nil
}

func done(err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, toolErr(err)
	}
	return nil, map[string]bool{"ok": true}, nil
}

func toolErr(err error) error {
	if app.ErrKind(err) != 0 {
		return err
	}
	if strings.Contains(err.Error(), "OpenAI") {
		return err
	}
	return errors.New("internal server error")
}

// ---- inputs ----

type projectIn struct {
	ProjectID int64 `json:"project_id" jsonschema:"project id from list_projects"`
}

type nameIn struct {
	Name string `json:"name" jsonschema:"name"`
}

type renameProjectIn struct {
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name" jsonschema:"new name"`
}

type memberIn struct {
	ProjectID int64 `json:"project_id"`
	UserID    int64 `json:"user_id" jsonschema:"member user_id from list_members"`
}

type addMemberIn struct {
	ProjectID int64  `json:"project_id"`
	Email     string `json:"email" jsonschema:"email of a registered user"`
	Role      string `json:"role" jsonschema:"admin or participant"`
}

type setRoleIn struct {
	ProjectID int64  `json:"project_id"`
	UserID    int64  `json:"user_id"`
	Role      string `json:"role" jsonschema:"admin or participant"`
}

type listFolderIn struct {
	ProjectID int64  `json:"project_id"`
	FolderID  *int64 `json:"folder_id,omitempty" jsonschema:"folder id; omit for the root"`
}

type createFolderIn struct {
	ProjectID int64  `json:"project_id"`
	ParentID  *int64 `json:"parent_id,omitempty" jsonschema:"parent folder id; omit for the root"`
	Name      string `json:"name"`
}

type renameFolderIn struct {
	FolderID int64  `json:"folder_id"`
	Name     string `json:"name"`
}

type renameDocumentIn struct {
	DocumentID int64  `json:"document_id"`
	Name       string `json:"name"`
}

type itemIn struct {
	ItemType string `json:"item_type" jsonschema:"folder or document"`
	ItemID   int64  `json:"item_id"`
}

type moveIn struct {
	ItemType       string `json:"item_type" jsonschema:"folder or document"`
	ItemID         int64  `json:"item_id"`
	TargetFolderID *int64 `json:"target_folder_id,omitempty" jsonschema:"destination folder; omit for the project root"`
}

type uploadIn struct {
	ProjectID     int64  `json:"project_id"`
	Filename      string `json:"filename" jsonschema:"file name with extension (.pdf, .docx, .txt, .md)"`
	FolderID      *int64 `json:"folder_id,omitempty" jsonschema:"folder; omit for the root"`
	ContentBase64 string `json:"content_base64,omitempty" jsonschema:"file content in base64 (or url)"`
	URL           string `json:"url,omitempty" jsonschema:"http(s) link the server will download (or content_base64)"`
}

type getDocumentIn struct {
	DocumentID int64 `json:"document_id"`
	Pages      []int `json:"pages,omitempty" jsonschema:"page numbers whose text to return"`
	AllPages   bool  `json:"all_pages,omitempty" jsonschema:"return the text of all pages"`
}

type askIn struct {
	ProjectID int64  `json:"project_id"`
	Question  string `json:"question" jsonschema:"a self-contained natural-language question"`
	FolderID  *int64 `json:"folder_id,omitempty" jsonschema:"limit search to a folder (with subfolders)"`
}

type searchIn struct {
	ProjectID int64  `json:"project_id"`
	Query     string `json:"query" jsonschema:"search query"`
	FolderID  *int64 `json:"folder_id,omitempty" jsonschema:"limit search to a folder (with subfolders)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many fragments to return (default 8, max 50)"`
}

// ---- handlers ----

type R = *mcp.CallToolRequest

func (t tools) listProjects(ctx context.Context, _ R, _ struct{}) (*mcp.CallToolResult, any, error) {
	return ok(t.a.ListProjects(ctx, t.uid))
}

func (t tools) createProject(ctx context.Context, _ R, in nameIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.CreateProject(ctx, t.uid, in.Name))
}

func (t tools) renameProject(ctx context.Context, _ R, in renameProjectIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.RenameProject(ctx, t.uid, in.ProjectID, in.Name))
}

func (t tools) deleteProject(ctx context.Context, _ R, in projectIn) (*mcp.CallToolResult, any, error) {
	return done(t.a.DeleteProject(ctx, t.uid, in.ProjectID))
}

func (t tools) listMembers(ctx context.Context, _ R, in projectIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.ListMembers(ctx, t.uid, in.ProjectID))
}

func (t tools) addMember(ctx context.Context, _ R, in addMemberIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.AddMember(ctx, t.uid, in.ProjectID, in.Email, app.Role(in.Role)))
}

func (t tools) removeMember(ctx context.Context, _ R, in memberIn) (*mcp.CallToolResult, any, error) {
	return done(t.a.RemoveMember(ctx, t.uid, in.ProjectID, in.UserID))
}

func (t tools) setMemberRole(ctx context.Context, _ R, in setRoleIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.SetMemberRole(ctx, t.uid, in.ProjectID, in.UserID, app.Role(in.Role)))
}

func (t tools) listFolder(ctx context.Context, _ R, in listFolderIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.ListFolder(ctx, t.uid, in.ProjectID, in.FolderID))
}

func (t tools) createFolder(ctx context.Context, _ R, in createFolderIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.CreateFolder(ctx, t.uid, in.ProjectID, in.ParentID, in.Name))
}

func (t tools) renameFolder(ctx context.Context, _ R, in renameFolderIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.RenameFolder(ctx, t.uid, in.FolderID, in.Name))
}

func (t tools) renameDocument(ctx context.Context, _ R, in renameDocumentIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.RenameDocument(ctx, t.uid, in.DocumentID, in.Name))
}

func (t tools) moveItem(ctx context.Context, _ R, in moveIn) (*mcp.CallToolResult, any, error) {
	switch in.ItemType {
	case "folder":
		return ok(t.a.MoveFolder(ctx, t.uid, in.ItemID, in.TargetFolderID))
	case "document":
		return ok(t.a.MoveDocument(ctx, t.uid, in.ItemID, in.TargetFolderID))
	}
	return nil, nil, errors.New("item_type must be folder or document")
}

func (t tools) deleteItem(ctx context.Context, _ R, in itemIn) (*mcp.CallToolResult, any, error) {
	switch in.ItemType {
	case "folder":
		return done(t.a.DeleteFolder(ctx, t.uid, in.ItemID))
	case "document":
		return done(t.a.DeleteDocument(ctx, t.uid, in.ItemID))
	}
	return nil, nil, errors.New("item_type must be folder or document")
}

func (t tools) uploadDocument(ctx context.Context, _ R, in uploadIn) (*mcp.CallToolResult, any, error) {
	if (in.ContentBase64 == "") == (in.URL == "") {
		return nil, nil, errors.New("pass exactly one of: content_base64 or url")
	}
	var body io.Reader
	filename := in.Filename
	if in.ContentBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
		if err != nil {
			return nil, nil, errors.New("content_base64 is not valid base64")
		}
		body = strings.NewReader(string(raw))
	} else {
		rc, name, err := download(ctx, in.URL, t.a.Cfg.MaxUploadMB<<20)
		if err != nil {
			return nil, nil, err
		}
		defer rc.Close()
		body = rc
		if filename == "" {
			filename = name
		}
	}
	return ok(t.a.Upload(ctx, t.uid, in.ProjectID, in.FolderID, filename, body))
}

type documentOut struct {
	app.Document
	Pages []app.Page `json:"pages,omitempty"`
}

func (t tools) getDocument(ctx context.Context, _ R, in getDocumentIn) (*mcp.CallToolResult, any, error) {
	d, err := t.a.GetDocument(ctx, t.uid, in.DocumentID)
	if err != nil {
		return nil, nil, toolErr(err)
	}
	out := documentOut{Document: d}
	if in.AllPages || len(in.Pages) > 0 {
		pages, err := t.a.DocumentPages(ctx, t.uid, in.DocumentID)
		if err != nil {
			return nil, nil, toolErr(err)
		}
		want := map[int]bool{}
		for _, p := range in.Pages {
			want[p] = true
		}
		out.Pages = []app.Page{}
		for _, p := range pages {
			if in.AllPages || want[p.Page] {
				out.Pages = append(out.Pages, p)
			}
		}
	}
	return ok(out, nil)
}

func (t tools) askProject(ctx context.Context, _ R, in askIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.Ask(ctx, t.uid, in.ProjectID, in.Question, in.FolderID))
}

func (t tools) searchProject(ctx context.Context, _ R, in searchIn) (*mcp.CallToolResult, any, error) {
	return ok(t.a.Search(ctx, t.uid, in.ProjectID, in.Query, in.FolderID, in.Limit))
}

// ---- url download ----

// download fetches a public http(s) URL, refusing private, loopback and link-local addresses.
func download(ctx context.Context, raw string, limit int64) (io.ReadCloser, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", errors.New("url must be an http(s) link")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("address %s is not allowed for download", host)
		}
		return nil
	}}
	client := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, "", fmt.Errorf("failed to download file: HTTP %d", res.StatusCode)
	}
	name := path.Base(res.Request.URL.Path)
	if name == "/" || name == "." {
		name = ""
	}
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(res.Body, limit+1), res.Body}, name, nil
}
