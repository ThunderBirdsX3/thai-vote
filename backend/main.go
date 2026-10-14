package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type config struct {
	DatabaseURL  string
	Pepper       []byte
	Origin       string
	MockThaiID   bool
	SecureCookie bool
	Addr         string
}

func loadConfig() config {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	c := config{
		DatabaseURL:  get("DATABASE_URL", "postgres://thaivote:thaivote@localhost:5432/thaivote?sslmode=disable"),
		Pepper:       []byte(get("ID_PEPPER", "dev-pepper-change-me")),
		Origin:       get("APP_ORIGIN", "http://localhost:5173"),
		MockThaiID:   get("MOCK_THAIID", "false") == "true",
		SecureCookie: get("COOKIE_SECURE", "false") == "true",
		Addr:         get("ADDR", ":8080"),
	}
	if c.SecureCookie && string(c.Pepper) == "dev-pepper-change-me" {
		log.Fatal("ID_PEPPER must be set when COOKIE_SECURE=true (production)")
	}
	return c
}

// ---- JSON / error helpers ----

type apiErr struct {
	Status int
	Msg    string
}

func (e *apiErr) Error() string { return e.Msg }

func bad(msg string) error      { return &apiErr{http.StatusBadRequest, msg} }
func unauth(msg string) error   { return &apiErr{http.StatusUnauthorized, msg} }
func notFound(msg string) error { return &apiErr{http.StatusNotFound, msg} }
func conflict(msg string) error { return &apiErr{http.StatusConflict, msg} }
func forbid(msg string) error   { return &apiErr{http.StatusForbidden, msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bad("invalid JSON body")
	}
	return nil
}

// h adapts an error-returning handler.
func h(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			var ae *apiErr
			if errors.As(err, &ae) {
				writeJSON(w, ae.Status, map[string]string{"error": ae.Msg})
				return
			}
			log.Printf("ERROR %s %s: %v", r.Method, r.URL.Path, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
	}
}

// ---- middleware ----

// originCheck blocks cross-site state-changing requests (CSRF) on top of SameSite cookies.
func (a *app) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != a.cfg.Origin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "bad origin"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type limiter struct {
	mu sync.Mutex
	m  map[string]*rate.Limiter
	r  rate.Limit
	b  int
}

func newLimiter(every time.Duration, burst int) *limiter {
	return &limiter{m: map[string]*rate.Limiter{}, r: rate.Every(every), b: burst}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.m[key]
	if !ok {
		if len(l.m) > 10000 { // ponytail: crude cap against memory growth; use a TTL map if needed
			l.m = map[string]*rate.Limiter{}
		}
		lim = rate.NewLimiter(l.r, l.b)
		l.m[key] = lim
	}
	return lim.Allow()
}

// clientIP trusts X-Forwarded-For only from the nginx proxy (always in front in compose).
func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Real-IP"); xf != "" {
		return xf
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func limit(l *limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
			return
		}
		next(w, r)
	}
}

// ---- app / routes ----

type app struct {
	cfg        config
	db         dbPool
	loginLimit *limiter
	voteLimit  *limiter
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	// voter
	mux.HandleFunc("POST /api/auth/mock-thaiid", limit(a.loginLimit, h(a.handleMockThaiID)))
	mux.HandleFunc("POST /api/auth/logout", h(a.handleLogout))
	mux.HandleFunc("GET /api/me", h(a.handleMe))
	mux.HandleFunc("GET /api/polls", h(a.handleListPolls))
	mux.HandleFunc("GET /api/polls/{id}", h(a.handleGetPoll))
	mux.HandleFunc("POST /api/polls/{id}/vote", limit(a.voteLimit, h(a.handleVote)))
	mux.HandleFunc("GET /api/polls/{id}/results", h(a.handlePublicResults))
	mux.HandleFunc("GET /api/receipts/{code}", limit(a.voteLimit, h(a.handleReceipt)))

	// admin
	mux.HandleFunc("POST /api/admin/login", limit(a.loginLimit, h(a.handleAdminLogin)))
	mux.HandleFunc("POST /api/admin/logout", h(a.handleAdminLogout))
	mux.HandleFunc("GET /api/admin/me", h(a.handleAdminMe))
	mux.HandleFunc("GET /api/admin/polls", h(a.adminOnly(a.handleAdminListPolls)))
	mux.HandleFunc("POST /api/admin/polls", h(a.adminOnly(a.handleAdminCreatePoll)))
	mux.HandleFunc("GET /api/admin/polls/{id}", h(a.adminOnly(a.handleAdminGetPoll)))
	mux.HandleFunc("PUT /api/admin/polls/{id}", h(a.adminOnly(a.handleAdminUpdatePoll)))
	mux.HandleFunc("DELETE /api/admin/polls/{id}", h(a.adminOnly(a.handleAdminDeletePoll)))
	mux.HandleFunc("POST /api/admin/polls/{id}/publish", h(a.adminOnly(a.handleAdminPublish)))
	mux.HandleFunc("POST /api/admin/polls/{id}/close", h(a.adminOnly(a.handleAdminClose)))
	mux.HandleFunc("GET /api/admin/polls/{id}/results", h(a.adminOnly(a.handleAdminResults)))
	mux.HandleFunc("GET /api/admin/polls/{id}/verify", h(a.adminOnly(a.handleAdminVerify)))
	mux.HandleFunc("POST /api/admin/ballots/{id}/hide", h(a.adminOnly(a.handleAdminHideComment)))
	mux.HandleFunc("GET /api/admin/audit-log", h(a.adminOnly(a.handleAdminAudit)))

	return a.originCheck(mux)
}

func main() {
	cfg := loadConfig()
	ctx := context.Background()

	pool, err := openDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	a := &app{
		cfg:        cfg,
		db:         pool,
		loginLimit: newLimiter(6*time.Second, 5), // ~10/min sustained, burst 5
		voteLimit:  newLimiter(time.Second, 10),
	}

	if len(os.Args) > 1 && os.Args[1] == "create-admin" {
		if err := a.cliCreateAdmin(ctx, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	go a.sessionJanitor(ctx)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	log.Printf("listening on %s (mock thaiid: %v)", cfg.Addr, cfg.MockThaiID)
	log.Fatal(srv.ListenAndServe())
}

// cliCreateAdmin: `api create-admin <username> [display name]`, password read from stdin.
func (a *app) cliCreateAdmin(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: create-admin <username> [display name] (password on stdin)")
	}
	name := args[0]
	if len(args) > 1 {
		name = strings.Join(args[1:], " ")
	}
	fmt.Fprint(os.Stderr, "password: ")
	pw, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	pw = strings.TrimRight(pw, "\r\n")
	if len(pw) < 10 {
		return fmt.Errorf("password must be at least 10 characters")
	}
	hash, err := hashPassword(pw)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `INSERT INTO admins (username, password_hash, display_name) VALUES ($1,$2,$3)`, args[0], hash, name)
	if err != nil {
		return err
	}
	fmt.Println("admin created:", args[0])
	return nil
}
