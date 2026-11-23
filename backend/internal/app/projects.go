package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *App) ListProjects(ctx context.Context, uid int64) ([]Project, error) {
	rows, err := a.DB.Query(ctx, `
		SELECT p.id, p.name, m.role, p.created_at FROM projects p JOIN memberships m ON m.project_id = p.id
		WHERE m.user_id = $1 ORDER BY p.created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Role, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (a *App) GetProject(ctx context.Context, uid, id int64) (Project, error) {
	role, err := a.authorize(ctx, a.DB, uid, id, RoleParticipant)
	if err != nil {
		return Project{}, err
	}
	p := Project{ID: id, Role: role}
	err = a.DB.QueryRow(ctx, `SELECT name, created_at FROM projects WHERE id=$1`, id).Scan(&p.Name, &p.CreatedAt)
	return p, err
}

func cleanName(s, what string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errf(KindBadRequest, "%s name is required", what)
	}
	if len([]rune(s)) > 255 {
		return "", errf(KindBadRequest, "%s name is too long", what)
	}
	return s, nil
}

// CreateProject creates a Project; the creator becomes its first Admin.
func (a *App) CreateProject(ctx context.Context, uid int64, name string) (Project, error) {
	name, err := cleanName(name, "project")
	if err != nil {
		return Project{}, err
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)
	p := Project{Name: name, Role: RoleAdmin}
	if err := tx.QueryRow(ctx, `INSERT INTO projects(name, created_by) VALUES ($1,$2) RETURNING id, created_at`, name, uid).
		Scan(&p.ID, &p.CreatedAt); err != nil {
		return Project{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(project_id, user_id, role) VALUES ($1,$2,'admin')`, p.ID, uid); err != nil {
		return Project{}, err
	}
	return p, tx.Commit(ctx)
}

func (a *App) RenameProject(ctx context.Context, uid, id int64, name string) (Project, error) {
	if _, err := a.authorize(ctx, a.DB, uid, id, RoleAdmin); err != nil {
		return Project{}, err
	}
	name, err := cleanName(name, "project")
	if err != nil {
		return Project{}, err
	}
	if _, err := a.DB.Exec(ctx, `UPDATE projects SET name=$2 WHERE id=$1`, id, name); err != nil {
		return Project{}, err
	}
	return a.GetProject(ctx, uid, id)
}

func (a *App) DeleteProject(ctx context.Context, uid, id int64) error {
	if _, err := a.authorize(ctx, a.DB, uid, id, RoleAdmin); err != nil {
		return err
	}
	if _, err := a.DB.Exec(ctx, `DELETE FROM projects WHERE id=$1`, id); err != nil {
		return err
	}
	_ = os.RemoveAll(a.projectDir(id))
	return nil
}

func (a *App) projectDir(id int64) string {
	return filepath.Join(a.Cfg.DataDir, "files", strconv.FormatInt(id, 10))
}

// ---- members ----

func (a *App) ListMembers(ctx context.Context, uid, projectID int64) ([]Member, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleParticipant); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `
		SELECT u.id, u.email, u.name, m.role FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.project_id=$1 ORDER BY m.role, u.email`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember adds an already registered User by email.
func (a *App) AddMember(ctx context.Context, uid, projectID int64, email string, role Role) (Member, error) {
	if _, err := a.authorize(ctx, a.DB, uid, projectID, RoleAdmin); err != nil {
		return Member{}, err
	}
	if _, err := ParseRole(string(role)); err != nil {
		return Member{}, err
	}
	m := Member{Role: role}
	err := a.DB.QueryRow(ctx, `SELECT id, email, name FROM users WHERE email=$1`, normEmail(email)).Scan(&m.UserID, &m.Email, &m.Name)
	if isNoRows(err) {
		return Member{}, errf(KindNotFound, "user %s is not registered", normEmail(email))
	}
	if err != nil {
		return Member{}, err
	}
	tag, err := a.DB.Exec(ctx, `INSERT INTO memberships(project_id, user_id, role) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		projectID, m.UserID, role)
	if err != nil {
		return Member{}, err
	}
	if tag.RowsAffected() == 0 {
		return Member{}, errf(KindConflict, "%s is already a member of the project", m.Email)
	}
	return m, nil
}

// SetMemberRole changes a Member's Role; the last Admin cannot be demoted.
func (a *App) SetMemberRole(ctx context.Context, uid, projectID, memberID int64, role Role) (Member, error) {
	if _, err := ParseRole(string(role)); err != nil {
		return Member{}, err
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return Member{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := a.authorize(ctx, tx, uid, projectID, RoleAdmin); err != nil {
		return Member{}, err
	}
	if err := a.lockAdmins(ctx, tx, projectID, memberID, role != RoleAdmin); err != nil {
		return Member{}, err
	}
	var m Member
	err = tx.QueryRow(ctx, `
		UPDATE memberships m SET role=$3 FROM users u
		WHERE m.project_id=$1 AND m.user_id=$2 AND u.id=m.user_id
		RETURNING u.id, u.email, u.name, m.role`, projectID, memberID, role).Scan(&m.UserID, &m.Email, &m.Name, &m.Role)
	if isNoRows(err) {
		return Member{}, errf(KindNotFound, "member not found")
	}
	if err != nil {
		return Member{}, err
	}
	return m, tx.Commit(ctx)
}

func (a *App) RemoveMember(ctx context.Context, uid, projectID, memberID int64) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := a.authorize(ctx, tx, uid, projectID, RoleAdmin); err != nil {
		return err
	}
	if err := a.lockAdmins(ctx, tx, projectID, memberID, true); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM memberships WHERE project_id=$1 AND user_id=$2`, projectID, memberID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errf(KindNotFound, "member not found")
	}
	return tx.Commit(ctx)
}

// lockAdmins locks the project's admin rows and refuses to drop the last Admin.
func (a *App) lockAdmins(ctx context.Context, tx pgx.Tx, projectID, memberID int64, losingAdmin bool) error {
	if !losingAdmin {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT user_id FROM memberships WHERE project_id=$1 AND role='admin' FOR UPDATE`, projectID)
	if err != nil {
		return err
	}
	admins, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	if len(admins) == 1 && admins[0] == memberID {
		return errf(KindConflict, "cannot remove or demote the last Admin of the project")
	}
	return nil
}
