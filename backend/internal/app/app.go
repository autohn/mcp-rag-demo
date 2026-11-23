// Package app holds every business operation. REST and MCP are thin adapters over it,
// and every operation goes through the same authorization function (authorize).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-rag-demo/backend/internal/config"
	"mcp-rag-demo/backend/internal/llm"
)

type App struct {
	DB    *pgxpool.Pool
	Cfg   config.Config
	LLM   *llm.Client
	queue chan int64
}

func New(db *pgxpool.Pool, cfg config.Config, client *llm.Client) *App {
	return &App{DB: db, Cfg: cfg, LLM: client, queue: make(chan int64, 1024)}
}

// ---- errors ----

type Kind int

const (
	KindBadRequest Kind = iota + 1
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
	KindTooLarge
	KindUnavailable
)

// Error is a user-facing error with a Russian message.
type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errf(k Kind, format string, args ...any) error {
	return &Error{Kind: k, Msg: fmt.Sprintf(format, args...)}
}

func ErrKind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// ---- roles & authorization ----

type Role string

const (
	RoleAdmin       Role = "admin"
	RoleParticipant Role = "participant"
)

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleAdmin, RoleParticipant:
		return Role(s), nil
	}
	return "", errf(KindBadRequest, "role must be admin or participant")
}

// authorize is the single authorization check for REST and MCP. It returns the caller's role
// in the project, or an error if the caller is not a Member or lacks the needed Role.
func (a *App) authorize(ctx context.Context, q querier, uid, projectID int64, need Role) (Role, error) {
	var role Role
	var name string
	err := q.QueryRow(ctx, `
		SELECT m.role, p.name FROM memberships m JOIN projects p ON p.id = m.project_id
		WHERE m.project_id = $1 AND m.user_id = $2`, projectID, uid).Scan(&role, &name)
	if isNoRows(err) {
		return "", errf(KindNotFound, "project %d not found or you have no access to it", projectID)
	}
	if err != nil {
		return "", err
	}
	if need == RoleAdmin && role != RoleAdmin {
		return "", errf(KindForbidden, "Admin role required in project \"%s\"", name)
	}
	return role, nil
}

// querier is satisfied by both the pool and a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ---- shared types ----

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type Project struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Member struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
}

type Folder struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
}

type Document struct {
	ID             int64     `json:"id"`
	ProjectID      int64     `json:"project_id"`
	FolderID       *int64    `json:"folder_id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Mime           string    `json:"mime"`
	Size           int64     `json:"size"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	PageCount      int       `json:"page_count"`
	ChunkCount     int       `json:"chunk_count"`
	Paged          bool      `json:"paged"`
	NoText         bool      `json:"no_text"`
	UploadedBy     *int64    `json:"uploaded_by"`
	UploadedByName string    `json:"uploaded_by_name"`
	CreatedAt      time.Time `json:"created_at"`
	path           string
}

type Page struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

type Fragment struct {
	ChunkID      int64   `json:"chunk_id"`
	DocumentID   int64   `json:"document_id"`
	DocumentName string  `json:"document_name"`
	Page         int     `json:"page"`
	Paged        bool    `json:"paged"`
	Text         string  `json:"text"`
	Score        float64 `json:"score"`
}

type Citation struct {
	N               int    `json:"n"`
	DocumentID      *int64 `json:"document_id"`
	DocumentName    string `json:"document_name"`
	Page            int    `json:"page"`
	Paged           bool   `json:"paged"`
	Quote           string `json:"quote"`
	DocumentDeleted bool   `json:"document_deleted"`
}

type QuestionRecord struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"project_id"`
	FolderID  *int64     `json:"folder_id"`
	Question  string     `json:"question"`
	Answer    string     `json:"answer"`
	Found     bool       `json:"found"`
	Citations []Citation `json:"citations"`
	CreatedAt time.Time  `json:"created_at"`
}

func logErr(msg string, err error, args ...any) {
	slog.Error(msg, append([]any{"err", err}, args...)...)
}
