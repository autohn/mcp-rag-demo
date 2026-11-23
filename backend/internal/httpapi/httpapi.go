// Package httpapi exposes the app over REST (cookie sessions) and serves the built frontend.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mcp-rag-demo/backend/internal/app"
)

const cookieName = "session"

type Server struct {
	app *app.App
}

type ctxKey struct{}

func userFrom(ctx context.Context) app.User { return ctx.Value(ctxKey{}).(app.User) }

// Handler returns the /api routes plus the SPA. mcp is mounted at /mcp.
func Handler(a *app.App, mcp http.Handler) http.Handler {
	s := &Server{app: a}
	api := http.NewServeMux()

	api.HandleFunc("POST /api/auth/register", s.register)
	api.HandleFunc("POST /api/auth/login", s.login)
	api.HandleFunc("POST /api/auth/logout", s.logout)
	api.Handle("GET /api/me", s.auth(s.me))

	api.Handle("GET /api/projects", s.auth(s.listProjects))
	api.Handle("POST /api/projects", s.auth(s.createProject))
	api.Handle("GET /api/projects/{id}", s.auth(s.getProject))
	api.Handle("PATCH /api/projects/{id}", s.auth(s.renameProject))
	api.Handle("DELETE /api/projects/{id}", s.auth(s.deleteProject))

	api.Handle("GET /api/projects/{id}/members", s.auth(s.listMembers))
	api.Handle("POST /api/projects/{id}/members", s.auth(s.addMember))
	api.Handle("PATCH /api/projects/{id}/members/{userId}", s.auth(s.setMemberRole))
	api.Handle("DELETE /api/projects/{id}/members/{userId}", s.auth(s.removeMember))

	api.Handle("GET /api/projects/{id}/folders", s.auth(s.listFolders))
	api.Handle("POST /api/projects/{id}/folders", s.auth(s.createFolder))
	api.Handle("PATCH /api/folders/{id}", s.auth(s.patchFolder))
	api.Handle("DELETE /api/folders/{id}", s.auth(s.deleteFolder))

	api.Handle("GET /api/projects/{id}/documents", s.auth(s.listDocuments))
	api.Handle("POST /api/projects/{id}/documents", s.auth(s.uploadDocuments))
	api.Handle("GET /api/documents/{id}", s.auth(s.getDocument))
	api.Handle("GET /api/documents/{id}/file", s.auth(s.documentFile))
	api.Handle("GET /api/documents/{id}/pages", s.auth(s.documentPages))
	api.Handle("PATCH /api/documents/{id}", s.auth(s.patchDocument))
	api.Handle("DELETE /api/documents/{id}", s.auth(s.deleteDocument))

	api.Handle("POST /api/projects/{id}/questions", s.auth(s.ask))
	api.Handle("GET /api/projects/{id}/questions", s.auth(s.history))
	api.Handle("POST /api/projects/{id}/search", s.auth(s.search))

	api.Handle("GET /api/tokens", s.auth(s.listTokens))
	api.Handle("POST /api/tokens", s.auth(s.createToken))
	api.Handle("DELETE /api/tokens/{id}", s.auth(s.revokeToken))

	api.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"llm_enabled": a.LLM.Enabled(), "max_upload_mb": a.Cfg.MaxUploadMB})
	})
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &app.Error{Kind: app.KindNotFound, Msg: "no such API method"})
	})

	root := http.NewServeMux()
	root.Handle("/api/", http.NewCrossOriginProtection().Handler(api))
	root.Handle("/mcp", mcp)
	root.Handle("/", spa(a.Cfg.WebDir))
	return logRequests(root)
}

// ---- helpers ----

func (s *Server) auth(h func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, &app.Error{Kind: app.KindUnauthorized, Msg: "sign-in required"})
			return
		}
		u, err := s.app.UserBySession(r.Context(), c.Value)
		if err != nil {
			writeError(w, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := "internal server error"
	var e *app.Error
	if errors.As(err, &e) {
		msg = e.Msg
		status = map[app.Kind]int{
			app.KindBadRequest:   http.StatusBadRequest,
			app.KindUnauthorized: http.StatusUnauthorized,
			app.KindForbidden:    http.StatusForbidden,
			app.KindNotFound:     http.StatusNotFound,
			app.KindConflict:     http.StatusConflict,
			app.KindTooLarge:     http.StatusRequestEntityTooLarge,
			app.KindUnavailable:  http.StatusServiceUnavailable,
		}[e.Kind]
	} else {
		slog.Error("request failed", "err", err)
		if strings.Contains(err.Error(), "OpenAI") {
			status, msg = http.StatusBadGateway, err.Error()
		}
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return &app.Error{Kind: app.KindBadRequest, Msg: "invalid JSON in request"}
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, &app.Error{Kind: app.KindBadRequest, Msg: "invalid id"}
	}
	return id, nil
}

// optID parses an optional id from a query/form value; "" and "root" mean nil.
func optID(s string) (*int64, error) {
	if s == "" || s == "root" || s == "null" {
		return nil, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, &app.Error{Kind: app.KindBadRequest, Msg: "invalid folder id"}
	}
	return &id, nil
}

// optionalID distinguishes an absent JSON field from an explicit null.
type optionalID struct {
	Set   bool
	Value *int64
}

func (o *optionalID) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
}

// run handles the common "parse ids, call app, write JSON" shape.
func run(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, v)
}

// ---- auth ----

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil, MaxAge: maxAge,
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Name string }
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	u, err := s.app.Register(r.Context(), in.Email, in.Password, in.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	tok, err := s.app.NewSession(r.Context(), u.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.setSession(w, r, tok, int(app.SessionTTL/time.Second))
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	u, tok, err := s.app.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	s.setSession(w, r, tok, int(app.SessionTTL/time.Second))
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.app.Logout(r.Context(), c.Value)
	}
	s.setSession(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

// ---- projects & members ----

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListProjects(r.Context(), userFrom(r.Context()).ID)
	run(w, http.StatusOK, v, err)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.CreateProject(r.Context(), userFrom(r.Context()).ID, in.Name)
	run(w, http.StatusCreated, v, err)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.GetProject(r.Context(), userFrom(r.Context()).ID, id)
	run(w, http.StatusOK, v, err)
}

func (s *Server) renameProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct{ Name string }
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.RenameProject(r.Context(), userFrom(r.Context()).ID, id, in.Name)
	run(w, http.StatusOK, v, err)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err == nil {
		err = s.app.DeleteProject(r.Context(), userFrom(r.Context()).ID, id)
	}
	run(w, 0, nil, err)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.ListMembers(r.Context(), userFrom(r.Context()).ID, id)
	run(w, http.StatusOK, v, err)
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Email string
		Role  app.Role
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.AddMember(r.Context(), userFrom(r.Context()).ID, id, in.Email, in.Role)
	run(w, http.StatusCreated, v, err)
}

func (s *Server) setMemberRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var uid int64
	var in struct{ Role app.Role }
	if err == nil {
		uid, err = pathID(r, "userId")
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.SetMemberRole(r.Context(), userFrom(r.Context()).ID, id, uid, in.Role)
	run(w, http.StatusOK, v, err)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var uid int64
	if err == nil {
		uid, err = pathID(r, "userId")
	}
	if err == nil {
		err = s.app.RemoveMember(r.Context(), userFrom(r.Context()).ID, id, uid)
	}
	run(w, 0, nil, err)
}

// ---- folders ----

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	uid := userFrom(r.Context()).ID
	if r.URL.Query().Get("all") != "" {
		v, err := s.app.AllFolders(r.Context(), uid, id)
		run(w, http.StatusOK, v, err)
		return
	}
	parent, err := optID(r.URL.Query().Get("parent"))
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.ListFolder(r.Context(), uid, id, parent)
	run(w, http.StatusOK, v.Folders, err)
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Name     string
		ParentID *int64 `json:"parent_id"`
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.CreateFolder(r.Context(), userFrom(r.Context()).ID, id, in.ParentID, in.Name)
	run(w, http.StatusCreated, v, err)
}

func (s *Server) patchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Name     *string
		ParentID optionalID `json:"parent_id"`
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	uid := userFrom(r.Context()).ID
	var f app.Folder
	if in.Name != nil {
		if f, err = s.app.RenameFolder(r.Context(), uid, id, *in.Name); err != nil {
			writeError(w, err)
			return
		}
	}
	if in.ParentID.Set {
		f, err = s.app.MoveFolder(r.Context(), uid, id, in.ParentID.Value)
	}
	run(w, http.StatusOK, f, err)
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err == nil {
		err = s.app.DeleteFolder(r.Context(), userFrom(r.Context()).ID, id)
	}
	run(w, 0, nil, err)
}

// ---- documents ----

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var folder *int64
	if err == nil {
		folder, err = optID(r.URL.Query().Get("folder"))
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.ListFolder(r.Context(), userFrom(r.Context()).ID, id, folder)
	run(w, http.StatusOK, v, err)
}

// uploadDocuments accepts one or more "file" parts plus an optional "folder_id" part, streaming to disk.
func (s *Server) uploadDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, &app.Error{Kind: app.KindBadRequest, Msg: "multipart/form-data expected"})
		return
	}
	uid := userFrom(r.Context()).ID
	var folder *int64
	out := []app.Document{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, &app.Error{Kind: app.KindBadRequest, Msg: "failed to read upload"})
			return
		}
		switch part.FormName() {
		case "folder_id":
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			if folder, err = optID(strings.TrimSpace(string(b))); err != nil {
				writeError(w, err)
				return
			}
		case "file":
			d, err := s.app.Upload(r.Context(), uid, id, folder, part.FileName(), part)
			if err != nil {
				writeError(w, err)
				return
			}
			out = append(out, d)
		}
		part.Close()
	}
	if len(out) == 0 {
		writeError(w, &app.Error{Kind: app.KindBadRequest, Msg: "no file provided"})
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.GetDocument(r.Context(), userFrom(r.Context()).ID, id)
	run(w, http.StatusOK, v, err)
}

func (s *Server) documentFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	d, f, err := s.app.OpenDocumentFile(r.Context(), userFrom(r.Context()).ID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	defer f.Close()
	disp := "inline"
	if r.URL.Query().Get("download") != "" {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", d.Mime)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": d.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", d.CreatedAt, f)
}

func (s *Server) documentPages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.DocumentPages(r.Context(), userFrom(r.Context()).ID, id)
	run(w, http.StatusOK, v, err)
}

func (s *Server) patchDocument(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Name     *string
		FolderID optionalID `json:"folder_id"`
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	uid := userFrom(r.Context()).ID
	var d app.Document
	if in.Name != nil {
		if d, err = s.app.RenameDocument(r.Context(), uid, id, *in.Name); err != nil {
			writeError(w, err)
			return
		}
	}
	if in.FolderID.Set {
		d, err = s.app.MoveDocument(r.Context(), uid, id, in.FolderID.Value)
	}
	run(w, http.StatusOK, d, err)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err == nil {
		err = s.app.DeleteDocument(r.Context(), userFrom(r.Context()).ID, id)
	}
	run(w, 0, nil, err)
}

// ---- questions ----

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Question string
		FolderID *int64 `json:"folder_id"`
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.Ask(r.Context(), userFrom(r.Context()).ID, id, in.Question, in.FolderID)
	run(w, http.StatusOK, v, err)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := s.app.History(r.Context(), userFrom(r.Context()).ID, id, limit)
	run(w, http.StatusOK, v, err)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	var in struct {
		Query    string
		FolderID *int64 `json:"folder_id"`
		Limit    int
	}
	if err == nil {
		err = decode(r, &in)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.Search(r.Context(), userFrom(r.Context()).ID, id, in.Query, in.FolderID, in.Limit)
	run(w, http.StatusOK, v, err)
}

// ---- tokens ----

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListAccessTokens(r.Context(), userFrom(r.Context()).ID)
	run(w, http.StatusOK, v, err)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := decode(r, &in); err != nil {
		writeError(w, err)
		return
	}
	v, err := s.app.CreateAccessToken(r.Context(), userFrom(r.Context()).ID, in.Name)
	run(w, http.StatusCreated, v, err)
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err == nil {
		err = s.app.RevokeAccessToken(r.Context(), userFrom(r.Context()).ID, id)
	}
	run(w, 0, nil, err)
}

// ---- static SPA ----

func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintln(w, "Frontend is not built. Run `npm run dev` in web/ or `npm run build`.")
			return
		}
		p := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		h.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" {
			slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "took", time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
