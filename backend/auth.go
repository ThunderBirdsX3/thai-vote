package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

const (
	voterCookie = "tv_session"
	adminCookie = "tv_admin"
	voterTTL    = 24 * time.Hour
	adminTTL    = 8 * time.Hour
)

// ---- passwords (argon2id) ----

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(pw, encoded string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var th uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &th); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[4])
	want, err2 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, th, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash makes unknown-username logins cost the same as wrong-password ones.
var dummyHash, _ = hashPassword("dummy-password-for-timing")

// ---- sessions ----

func sha(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

func randToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, sha([]byte(tok)), nil
}

func (a *app) startSession(ctx context.Context, w http.ResponseWriter, kind string, subject string) error {
	tok, hash, err := randToken()
	if err != nil {
		return err
	}
	name, ttl := voterCookie, voterTTL
	if kind == "admin" {
		name, ttl = adminCookie, adminTTL
	}
	if _, err := a.db.Exec(ctx,
		`INSERT INTO sessions (token_hash, kind, subject_id, expires_at) VALUES ($1,$2,$3,now()+$4::interval)`,
		hash, kind, subject, fmt.Sprintf("%d seconds", int(ttl.Seconds()))); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: tok, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *app) endSession(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	if c, err := r.Cookie(name); err == nil {
		_, _ = a.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, sha([]byte(c.Value)))
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteLaxMode})
}

// sessionSubject returns the subject id for a valid session cookie, or "".
func (a *app) sessionSubject(r *http.Request, name, kind string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	var id string
	err = a.db.QueryRow(r.Context(),
		`SELECT subject_id::text FROM sessions WHERE token_hash=$1 AND kind=$2 AND expires_at>now()`,
		sha([]byte(c.Value)), kind).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

func (a *app) voterID(r *http.Request) string { return a.sessionSubject(r, voterCookie, "voter") }
func (a *app) adminID(r *http.Request) string { return a.sessionSubject(r, adminCookie, "admin") }

type adminHandler func(w http.ResponseWriter, r *http.Request, adminID string) error

func (a *app) adminOnly(fn adminHandler) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := a.adminID(r)
		if id == "" {
			return unauth("admin login required")
		}
		return fn(w, r, id)
	}
}

func (a *app) sessionJanitor(ctx context.Context) {
	for {
		_, _ = a.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

// ---- voter login (ThaiID) ----

var citizenIDRe = regexp.MustCompile(`^\d{13}$`)

// validCitizenID checks the Thai national ID checksum.
func validCitizenID(id string) bool {
	if !citizenIDRe.MatchString(id) {
		return false
	}
	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(id[i]-'0') * (13 - i)
	}
	return (11-sum%11)%10 == int(id[12]-'0')
}

func maskCitizenID(id string) string {
	return fmt.Sprintf("%s-xxxx-xxxxx-%s-%s", id[:1], id[10:12], id[12:])
}

func (a *app) hashCitizenID(id string) []byte {
	m := hmac.New(sha256.New, a.cfg.Pepper)
	m.Write([]byte(id))
	return m.Sum(nil)
}

// loginVoter is the single entry point for voter identity. The mock handler calls it
// today; a real ThaiID OIDC callback should call it with the verified citizen id + name.
func (a *app) loginVoter(ctx context.Context, w http.ResponseWriter, citizenID, name string) (string, error) {
	if !validCitizenID(citizenID) {
		return "", bad("invalid citizen id")
	}
	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO voters (citizen_id_hash, citizen_id_masked, display_name, last_login_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (citizen_id_hash) DO UPDATE SET display_name=EXCLUDED.display_name, last_login_at=now()
		RETURNING id::text`, a.hashCitizenID(citizenID), maskCitizenID(citizenID), name).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, a.startSession(ctx, w, "voter", id)
}

func (a *app) handleMockThaiID(w http.ResponseWriter, r *http.Request) error {
	if !a.cfg.MockThaiID {
		return notFound("not found")
	}
	var in struct {
		CitizenID string `json:"citizen_id"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 100 {
		return bad("name must be 1-100 characters")
	}
	id, err := a.loginVoter(r.Context(), w, in.CitizenID, in.Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "name": in.Name, "citizen_id": maskCitizenID(in.CitizenID)})
	return nil
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) error {
	a.endSession(r.Context(), w, r, voterCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) error {
	id := a.voterID(r)
	if id == "" {
		return unauth("not logged in")
	}
	var name, masked string
	if err := a.db.QueryRow(r.Context(),
		`SELECT display_name, citizen_id_masked FROM voters WHERE id=$1`, id).Scan(&name, &masked); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "name": name, "citizen_id": masked})
	return nil
}

// ---- admin login ----

func (a *app) handleAdminLogin(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var id, hash, name string
	err := a.db.QueryRow(r.Context(),
		`SELECT id::text, password_hash, display_name FROM admins WHERE username=$1`, in.Username).Scan(&id, &hash, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		verifyPassword(in.Password, dummyHash)
		return unauth("invalid username or password")
	}
	if err != nil {
		return err
	}
	if !verifyPassword(in.Password, hash) {
		a.audit(r.Context(), id, "admin.login_failed", in.Username, nil)
		return unauth("invalid username or password")
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE admins SET last_login_at=now() WHERE id=$1`, id)
	if err := a.startSession(r.Context(), w, "admin", id); err != nil {
		return err
	}
	a.audit(r.Context(), id, "admin.login", in.Username, nil)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "username": in.Username, "name": name})
	return nil
}

func (a *app) handleAdminLogout(w http.ResponseWriter, r *http.Request) error {
	a.endSession(r.Context(), w, r, adminCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleAdminMe(w http.ResponseWriter, r *http.Request) error {
	id := a.adminID(r)
	if id == "" {
		return unauth("not logged in")
	}
	var username, name string
	if err := a.db.QueryRow(r.Context(),
		`SELECT username, display_name FROM admins WHERE id=$1`, id).Scan(&username, &name); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "username": username, "name": name})
	return nil
}
