package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const SessionTTL = 30 * 24 * time.Hour

func randomToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (a *App) Register(ctx context.Context, email, password, name string) (User, error) {
	email = normEmail(email)
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		return User{}, errf(KindBadRequest, "invalid email")
	}
	if len(password) < 6 {
		return User{}, errf(KindBadRequest, "password must be at least 6 characters")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{Email: email, Name: name}
	err = a.DB.QueryRow(ctx, `INSERT INTO users(email, password_hash, name) VALUES ($1,$2,$3) RETURNING id`,
		email, string(hash), name).Scan(&u.ID)
	if pe, ok := err.(*pgconn.PgError); ok && pe.Code == "23505" {
		return User{}, errf(KindConflict, "a user with this email is already registered")
	}
	return u, err
}

// Login checks credentials and returns a new session token.
func (a *App) Login(ctx context.Context, email, password string) (User, string, error) {
	var u User
	var hash string
	err := a.DB.QueryRow(ctx, `SELECT id, email, name, password_hash FROM users WHERE email=$1`, normEmail(email)).
		Scan(&u.ID, &u.Email, &u.Name, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		if err != nil && !isNoRows(err) {
			return User{}, "", err
		}
		return User{}, "", errf(KindUnauthorized, "wrong email or password")
	}
	tok, err := a.NewSession(ctx, u.ID)
	return u, tok, err
}

func (a *App) NewSession(ctx context.Context, uid int64) (string, error) {
	tok := randomToken("s_")
	_, err := a.DB.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		hashToken(tok), uid, time.Now().Add(SessionTTL))
	return tok, err
}

func (a *App) Logout(ctx context.Context, token string) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hashToken(token))
	return err
}

func (a *App) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	err := a.DB.QueryRow(ctx, `
		SELECT u.id, u.email, u.name FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash=$1 AND s.expires_at > now()`, hashToken(token)).Scan(&u.ID, &u.Email, &u.Name)
	if isNoRows(err) {
		return User{}, errf(KindUnauthorized, "sign-in required")
	}
	return u, err
}

// ---- access tokens ----

type AccessToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Token      string     `json:"token,omitempty"` // only on creation
}

func (a *App) UserByAccessToken(ctx context.Context, token string) (User, error) {
	var u User
	err := a.DB.QueryRow(ctx, `
		UPDATE access_tokens t SET last_used_at = now() FROM users u
		WHERE t.token_hash=$1 AND u.id = t.user_id
		RETURNING u.id, u.email, u.name`, hashToken(token)).Scan(&u.ID, &u.Email, &u.Name)
	if isNoRows(err) {
		return User{}, errf(KindUnauthorized, "invalid or revoked Access Token")
	}
	return u, err
}

func (a *App) CreateAccessToken(ctx context.Context, uid int64, name string) (AccessToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AccessToken{}, errf(KindBadRequest, "token name is required")
	}
	t := AccessToken{Name: name, Token: randomToken("rag_")}
	err := a.DB.QueryRow(ctx, `INSERT INTO access_tokens(user_id, name, token_hash) VALUES ($1,$2,$3) RETURNING id, created_at`,
		uid, name, hashToken(t.Token)).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (a *App) ListAccessTokens(ctx context.Context, uid int64) ([]AccessToken, error) {
	rows, err := a.DB.Query(ctx, `SELECT id, name, created_at, last_used_at FROM access_tokens WHERE user_id=$1 ORDER BY created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessToken{}
	for rows.Next() {
		var t AccessToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) RevokeAccessToken(ctx context.Context, uid, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM access_tokens WHERE id=$1 AND user_id=$2`, id, uid)
	if err == nil && tag.RowsAffected() == 0 {
		return errf(KindNotFound, "token not found")
	}
	return err
}
