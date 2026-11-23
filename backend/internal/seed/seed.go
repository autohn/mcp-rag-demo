// Package seed creates demo users and projects with sample Documents.
// Each demo project is seeded once (tracked in seed_log), so deleting it is permanent.
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

type folder struct {
	name  string
	files []string
}

type project struct {
	key     string // seed_log key
	name    string
	root    []string // files in the project root
	folders []folder
}

// Listed oldest first: the project list shows the newest (the CV) on top.
var projects = []project{
	{
		key:  "construction",
		name: "Residential Building 2",
		root: []string{"Регламент_приёмки_работ.md"},
		folders: []folder{
			{"Structural (KR)", []string{"ПЗ_Конструктивные_решения.pdf"}},
			{"Architectural (AR)", []string{"Спецификация_окон_и_дверей.pdf"}},
		},
	},
	{
		key:  "cv",
		name: "CV — Mikhail Aleksanian",
		root: []string{"Mikhail_Aleksanian_CV.pdf"},
	},
}

func Run(ctx context.Context, a *app.App) error {
	admin, err := ensureUser(ctx, a, AdminEmail, "Anna Admin")
	if err != nil {
		return err
	}
	if _, err := ensureUser(ctx, a, ParticipantEmail, "Peter Foreman"); err != nil {
		return err
	}
	for _, p := range projects {
		tag, err := a.DB.Exec(ctx, `INSERT INTO seed_log(name) VALUES ($1) ON CONFLICT DO NOTHING`, p.key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue // already seeded earlier
		}
		if err := seedProject(ctx, a, admin, p); err != nil {
			a.DB.Exec(ctx, `DELETE FROM seed_log WHERE name=$1`, p.key)
			return err
		}
		slog.Info("seeded demo project", "project", p.name, "admin", AdminEmail, "participant", ParticipantEmail, "password", Password)
	}
	return nil
}

func ensureUser(ctx context.Context, a *app.App, email, name string) (int64, error) {
	var id int64
	err := a.DB.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id)
	if err == nil {
		return id, nil
	}
	u, err := a.Register(ctx, email, Password, name)
	return u.ID, err
}

func seedProject(ctx context.Context, a *app.App, admin int64, p project) error {
	proj, err := a.CreateProject(ctx, admin, p.name)
	if err != nil {
		return err
	}
	if _, err := a.AddMember(ctx, admin, proj.ID, ParticipantEmail, app.RoleParticipant); err != nil {
		return err
	}
	if err := upload(ctx, a, admin, proj.ID, nil, p.root); err != nil {
		return err
	}
	for _, f := range p.folders {
		folder, err := a.CreateFolder(ctx, admin, proj.ID, nil, f.name)
		if err != nil {
			return err
		}
		if err := upload(ctx, a, admin, proj.ID, &folder.ID, f.files); err != nil {
			return err
		}
	}
	return nil
}

func upload(ctx context.Context, a *app.App, admin, projectID int64, folderID *int64, names []string) error {
	for _, name := range names {
		f, err := files.Open("files/" + name)
		if err != nil {
			return err
		}
		_, err = a.Upload(ctx, admin, projectID, folderID, name, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
