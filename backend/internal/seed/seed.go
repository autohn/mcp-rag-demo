// Package seed creates the demo users and the demo project (a CV to ask questions about).
// The project is seeded once (tracked in seed_log), so deleting it is permanent.
package seed

import (
	"context"
	"embed"
	"errors"
	"log/slog"

	"mcp-rag-demo/backend/internal/app"
)

//go:embed files/*
var files embed.FS

const (
	AdminEmail = "admin@demo.local"
	// DemoEmail is the Participant the "Open the demo project" button signs in as.
	DemoEmail = "prorab@demo.local"
	Password  = "demo1234"

	projectKey  = "cv"
	projectName = "CV — Mikhail Aleksanian"
	projectFile = "Mikhail_Aleksanian_CV.pdf"
)

func Run(ctx context.Context, a *app.App) error {
	admin, err := ensureUser(ctx, a, AdminEmail, "Anna Admin")
	if err != nil {
		return err
	}
	if _, err := ensureUser(ctx, a, DemoEmail, "Demo User"); err != nil {
		return err
	}
	tag, err := a.DB.Exec(ctx, `INSERT INTO seed_log(name) VALUES ($1) ON CONFLICT DO NOTHING`, projectKey)
	if err != nil || tag.RowsAffected() == 0 {
		return err // error, or already seeded earlier
	}
	pid, err := seedProject(ctx, a, admin)
	if err != nil {
		a.DB.Exec(ctx, `DELETE FROM seed_log WHERE name=$1`, projectKey)
		return err
	}
	if _, err := a.DB.Exec(ctx, `UPDATE seed_log SET project_id=$2 WHERE name=$1`, projectKey, pid); err != nil {
		return err
	}
	slog.Info("seeded demo project", "project", projectName, "admin", AdminEmail, "demo", DemoEmail, "password", Password)
	return nil
}

func ensureUser(ctx context.Context, a *app.App, email, name string) (int64, error) {
	var id int64
	if err := a.DB.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id); err == nil {
		return id, nil
	}
	u, err := a.Register(ctx, email, Password, name)
	return u.ID, err
}

func seedProject(ctx context.Context, a *app.App, admin int64) (int64, error) {
	p, err := a.CreateProject(ctx, admin, projectName)
	if err != nil {
		return 0, err
	}
	if _, err := a.AddMember(ctx, admin, p.ID, DemoEmail, app.RoleParticipant); err != nil {
		return 0, err
	}
	f, err := files.Open("files/" + projectFile)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	_, err = a.Upload(ctx, admin, p.ID, nil, projectFile, f)
	return p.ID, err
}

var ErrNoDemo = errors.New("the demo project is not available")

// DemoLogin opens a session for the demo user and returns the demo project to show.
func DemoLogin(ctx context.Context, a *app.App) (app.User, string, int64, error) {
	var u app.User
	var pid *int64
	err := a.DB.QueryRow(ctx, `
		SELECT u.id, u.email, u.name, s.project_id
		FROM users u
		JOIN seed_log s ON s.name = $2
		JOIN memberships m ON m.project_id = s.project_id AND m.user_id = u.id
		WHERE u.email = $1`, DemoEmail, projectKey).Scan(&u.ID, &u.Email, &u.Name, &pid)
	if err != nil || pid == nil {
		return app.User{}, "", 0, ErrNoDemo
	}
	tok, err := a.NewSession(ctx, u.ID)
	return u, tok, *pid, err
}
