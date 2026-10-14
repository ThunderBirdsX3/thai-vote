# Thai Vote — Design + Implementation Plan

## Context
Greenfield project in `/Volumes/myspace/thai-vote` (empty dir). Goal: a trustworthy, secure voting/opinion website that works "for real, to a degree".
- Backend Go, frontend Vue.js, PostgreSQL, run via docker compose.
- Voter login = ThaiID. Real ThaiID not available yet → mock login ("login as anyone"), built so a real ThaiID OIDC flow can be added later through the same `loginVoter()` path.
- Admins and voters live in separate tables. Admin creates polls with start/end time.
- UI: clean, attractive, light theme only, Thai language.

User decisions (from Q&A):
- Ballot secrecy: **admin picks per poll** (anonymous or public).
- Poll kinds: **single choice, multi choice (max N), approve/disapprove/abstain preset, free-text comment**.
- Admin auth: **username + password (argon2id)**, no 2FA for now.
- Results visibility: **admin picks per poll** — live, or only after close.
- Deploy: **docker compose** (postgres + api + web).

Toolchain present: Go 1.26, Node 24, Docker 29, psql 15.

## Trust & security model (core of the design)
1. **One person, one vote** — `participations(poll_id, voter_id)` PK; inserted in the same transaction as the ballot. DB unique constraint, not app logic, enforces it.
2. **Vote window enforced server-side** — `now()` from Postgres inside the vote transaction, checked against `starts_at`/`ends_at`/`closed_at`/`published_at`.
3. **Anonymous polls** — ballot row has `voter_id = NULL`, no timestamp; participation row for anonymous polls stores no timestamp either. Ballot ids are random UUIDs.
   `ponytail:` residual leak via physical row order/WAL for someone with raw DB access; upgrade path = mixnet / blind signatures if threat model needs DB-admin resistance.
4. **Tamper-evident ballots** — per-poll hash chain: `hash = sha256(prev_hash || poll_id || seq || sorted option ids || comment || receipt_hash)`. Poll row locked `FOR UPDATE` while casting to serialize seq.
   `ponytail:` per-poll lock serializes votes; fine for concept load, shard/batch if throughput matters.
5. **Immutability** — Postgres triggers reject UPDATE/DELETE on `ballots`, `ballot_choices`, `participations`.
6. **Voter receipt** — random 128-bit code shown once after voting; only its sha256 stored. `/verify` page confirms "your ballot is included (poll X, #seq)" — never shows the choice (avoids vote-buying proof).
7. **Admin verify** — recompute chain + check `count(participations) == count(ballots)`; results page shows total + chain head hash.
8. **Poll lock** — options/kind/anonymity editable only before `starts_at` (or while unpublished). Admin can close early (`closed_at`), cannot reopen or edit after start.
9. **Privacy** — never store raw citizen ID: `citizen_id_hash = HMAC-SHA256(PEPPER, id)` + masked form (`x-xxxx-xxxxx-12-3`) for display.
10. **Sessions** — random 32-byte token in HttpOnly, Secure (prod), SameSite=Lax cookie; only sha256 of token stored in `sessions`. Separate cookies for voter (`tv_session`) and admin (`tv_admin`), admin TTL 8h, voter 24h.
11. **CSRF** — state-changing requests require `Origin` matching configured `APP_ORIGIN` (+ SameSite). Same-origin via nginx proxy, so no CORS.
12. **Brute force** — per-IP rate limit on admin login and vote endpoints (`golang.org/x/time/rate`, in-memory). Argon2id (`golang.org/x/crypto/argon2`) for admin passwords, constant-time compare.
13. **Audit log** — every admin action (login, create/edit/publish/close poll, hide comment) → `audit_log`.
14. **Input validation** — length limits on title/description/options/comment, option count 2–20, max_choices bounds, ends_at > starts_at; security headers (CSP, X-Frame-Options, nosniff) set by nginx.
15. **Mock ThaiID gated** — route exists only when `MOCK_THAIID=true`; mock validates 13-digit Thai ID checksum (`(11 - Σ d_i·(14-i) mod 11) mod 10`).

## Data model (`backend/migrations/001_init.sql`)
- `admins(id uuid pk, username citext unique, password_hash, display_name, created_at, last_login_at)`
- `voters(id uuid pk, citizen_id_hash bytea unique, citizen_id_masked, display_name, created_at, last_login_at)`
- `sessions(token_hash bytea pk, kind 'admin'|'voter', subject_id uuid, expires_at, created_at)`
- `polls(id, title, description, kind 'single'|'multi'|'approve'|'comment', max_choices, anonymous bool, results_visibility 'live'|'after_close', starts_at, ends_at, published_at, closed_at, created_by → admins, created_at, updated_at, CHECK ends_at > starts_at)`
- `poll_options(id, poll_id, label, position)` — `approve` kind auto-creates เห็นด้วย / ไม่เห็นด้วย / งดออกเสียง
- `participations(poll_id, voter_id, created_at NULL for anonymous, PK(poll_id, voter_id))`
- `ballots(id uuid, poll_id, seq, voter_id NULL if anonymous, comment, receipt_hash unique, prev_hash, hash, created_at NULL if anonymous, UNIQUE(poll_id, seq))`
- `ballot_choices(ballot_id, option_id, PK)`
- `comment_hidden(ballot_id pk, admin_id, reason, created_at)` — moderation without mutating ballots
- `audit_log(id bigserial, admin_id, action, target, detail jsonb, created_at)`
- Triggers: no UPDATE/DELETE on ballots/ballot_choices/participations.

## API (Go 1.26 stdlib `net/http` ServeMux with method patterns, `pgx/v5`, `goose` embedded migrations)
Voter:
- `POST /api/auth/mock-thaiid` {citizen_id, name} (flag-gated) · `POST /api/auth/logout` · `GET /api/me`
- `GET /api/polls` (published; status upcoming/open/closed; has_voted) · `GET /api/polls/{id}`
- `POST /api/polls/{id}/vote` {option_ids[], comment} → {receipt}
- `GET /api/polls/{id}/results` (respects visibility; comments list excludes hidden; names only on public polls)
- `GET /api/receipts/{code}`
Admin (`/api/admin/...`): login/logout/me; polls CRUD (edit/delete only before start); `POST …/publish`, `POST …/close`; `GET …/results` (always visible to admin); `GET …/verify`; `POST /api/admin/ballots/{id}/hide`; `GET /api/admin/audit-log`.
CLI: `api create-admin <username>` reads password from stdin (bootstrap first admin; `docker compose exec api ...`).

Backend layout (single `package main`, few files):
`backend/main.go` (config, router, middleware: session, origin check, rate limit, JSON helpers) · `db.go` (pool, goose embed) · `auth.go` (sessions, admin login, argon2, mock ThaiID, `loginVoter`) · `polls.go` (voter + admin poll handlers, validation, status) · `vote.go` (cast vote tx, hash chain, receipt, verify) · `migrations/001_init.sql` · `vote_test.go` · `Dockerfile`.

## Frontend (`frontend/`: Vue 3 + Vite + TypeScript + Vue Router + Tailwind CSS v4)
- No Pinia — small `useAuth()` composable with reactive state. No chart lib — result bars in CSS.
- Font: IBM Plex Sans Thai (Google Fonts). Palette: white/slate-50 background, slate-900 text, one deep-blue accent, green/red/gray for approve/disapprove/abstain. Cards with 1px border, rounded-xl, generous spacing. Times displayed in Asia/Bangkok.
- Voter pages: `/` poll list (tabs: เปิดอยู่ / เร็วๆ นี้ / ปิดแล้ว) · `/login` "เข้าสู่ระบบด้วย ThaiID" → mock dialog (preset personas + random valid ID generator + editable name) · `/polls/:id` vote form → confirm dialog → receipt screen (copy button) · `/polls/:id/results` · `/verify`.
- Admin pages: `/admin/login` · `/admin` poll dashboard · `/admin/polls/new` & `/admin/polls/:id/edit` (kind, options, max, anonymous toggle, results visibility, `datetime-local` start/end) · `/admin/polls/:id` (results, comments + hide, verify button, publish/close) · `/admin/audit`.
- Router guards via `/api/me` and `/api/admin/me`.

## Infra
- `docker-compose.yml`: `db` (postgres:16, volume, healthcheck), `api` (Go multi-stage → distroless), `web` (Vite build → nginx serving SPA + proxy `/api` → api, security headers).
- `.env.example`: `DATABASE_URL`, `ID_PEPPER`, `APP_ORIGIN`, `MOCK_THAIID=true`, `COOKIE_SECURE`.
- Dev: `docker compose up db`, `go run .` in backend, `npm run dev` in frontend with Vite proxy `/api`.
- `README.md`: run steps, create-admin, security model summary.

## Execution order
1. Write spec copy to `docs/superpowers/specs/2026-10-14-thai-vote-design.md`, `git init`, commit.
2. Backend: migrations → db/main → auth → polls → vote → test.
3. Frontend: scaffold Vite, layout/theme, voter pages, admin pages.
4. Docker compose + nginx + README.
5. End-to-end check.

## Verification
- `go test ./...` in `backend/` against compose Postgres: `vote_test.go` covers duplicate vote rejected, vote outside window rejected, multi max enforced, anonymous ballot has no voter_id/timestamp, hash chain verifies and detects tampering, UPDATE on ballots blocked by trigger, Thai ID checksum.
- `npm run build` (includes `vue-tsc`) in `frontend/`.
- `docker compose up --build`; create admin via CLI; in browser pane: admin creates one poll of each kind (mix anonymous/public, live/after_close) → mock-login two voters → vote, get receipt, second vote rejected → verify receipt → results visibility behaves → admin verify shows chain OK → audit log shows actions. Screenshot key pages for UI check.

Skipped: real ThaiID OIDC (add a `/api/auth/thaiid/callback` calling `loginVoter`), admin 2FA, multi-question surveys, voter eligibility lists, email/notifications. Add when needed.
