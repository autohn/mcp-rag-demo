// Package seed creates a demo project with sample Documents on first start.
package seed

import (
	"context"
	"embed"
	"log/slog"

	"mcp-rag-demo/backend/internal/app"
)

//go:embed files/*
var files embed.FS

const (
	AdminEmail       = "admin@demo.local"
	ParticipantEmail = "prorab@demo.local"
	Password         = "demo1234"
)

// Run seeds once: it does nothing if the demo admin already exists.
func Run(ctx context.Context, a *app.App) error {
	var exists bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, AdminEmail).Scan(&exists); err != nil || exists {
		return err
	}
	admin, err := a.Register(ctx, AdminEmail, Password, "Anna Admin")
	if err != nil {
		return err
	}
	if _, err := a.Register(ctx, ParticipantEmail, Password, "Peter Foreman"); err != nil {
		return err
	}
	p, err := a.CreateProject(ctx, admin.ID, "Residential Building 2")
	if err != nil {
		return err
	}
	if _, err := a.AddMember(ctx, admin.ID, p.ID, ParticipantEmail, app.RoleParticipant); err != nil {
		return err
	}
	kr, err := a.CreateFolder(ctx, admin.ID, p.ID, nil, "Structural (KR)")
	if err != nil {
		return err
	}
	ar, err := a.CreateFolder(ctx, admin.ID, p.ID, nil, "Architectural (AR)")
	if err != nil {
		return err
	}
	place := map[string]*int64{
		"ПЗ_Конструктивные_решения.pdf":  &kr.ID,
		"Спецификация_окон_и_дверей.pdf": &ar.ID,
		"Регламент_приёмки_работ.md":     nil,
	}
	for name, folder := range place {
		f, err := files.Open("files/" + name)
		if err != nil {
			return err
		}
		_, err = a.Upload(ctx, admin.ID, p.ID, folder, name, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	slog.Info("seeded demo project", "admin", AdminEmail, "participant", ParticipantEmail, "password", Password)
	return nil
}
