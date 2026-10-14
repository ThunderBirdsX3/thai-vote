# โหวตไทย (Thai Vote)

Voting and opinion site. Go API + Vue 3 frontend + PostgreSQL. Light theme, Thai UI.

- Voters log in with ThaiID. **Real ThaiID is not connected yet**: with `MOCK_THAIID=true` the login page lets you pick any identity.
- Admins (separate `admins` table, password login) create polls with a start and end time.
- Poll kinds: single choice, multi choice (max N), approve / disapprove / abstain, free-text comment.
- Per poll, admin chooses: anonymous or named voters; results live or only after close.

## Run (Docker)

```bash
cp .env.example .env        # edit secrets
docker compose up -d --build
printf 'your-admin-password\n' | docker compose exec -T api /app/api create-admin admin "Admin name"
```

Open http://localhost:8080 (voters) and http://localhost:8080/admin/login (admins). Admin password: 10+ characters.

## Run (development)

```bash
docker run -d --name tv-db -e POSTGRES_USER=thaivote -e POSTGRES_PASSWORD=thaivote -e POSTGRES_DB=thaivote -p 55432:5432 postgres:16-alpine
cd backend && DATABASE_URL='postgres://thaivote:thaivote@localhost:55432/thaivote?sslmode=disable' MOCK_THAIID=true go run .
cd frontend && npm install && npm run dev      # http://localhost:5173, proxies /api to :8080
```

Tests (need a Postgres; defaults to `localhost:55432/thaivote_test`, override with `TEST_DATABASE_URL`):
`cd backend && go test ./...` — tests truncate that database, never point it at real data.

## How trust is enforced

| Guarantee | Mechanism |
|---|---|
| One person, one vote | `participations` primary key `(poll_id, voter_id)`, inserted in the same transaction as the ballot |
| Votes only inside the window | Status computed from the database clock inside the vote transaction |
| Anonymous ballots | Ballot has no `voter_id` and no timestamp; participation has no timestamp; anonymous results are tally-only (comments are shuffled) |
| Ballots can't be changed quietly | Triggers reject UPDATE/DELETE on ballots, choices, participations; per-poll SHA-256 hash chain; admin "verify" recomputes it |
| Voter can check their vote | One-time receipt code; only its hash is stored; `/verify` confirms inclusion without revealing the choice |
| Polls can't be changed after start | API rejects edits once started; only an open poll can be closed early; only unpublished drafts can be deleted |
| No raw citizen IDs | Stored as HMAC-SHA256 with `ID_PEPPER`, plus a masked form for display |
| Sessions and CSRF | Random tokens (only hashes stored), HttpOnly SameSite=Lax cookies, `Origin` must equal `APP_ORIGIN` |
| Admin accounts | Argon2id, rate-limited login, audit log of every admin action |

Known limits (by design for a first version):
- Someone with raw database write access can bypass the triggers. The hash chain detects this only if the head hash was recorded elsewhere (it is shown on the results page; save it).
- Anonymity holds against app users and admins, not against someone who can read physical storage or WAL.
- Rate limiting is in memory, per API instance.

## Connecting real ThaiID later

Add an OIDC callback in `backend/auth.go` that verifies the ThaiID response and calls `loginVoter(ctx, w, citizenID, name)`. Then set `MOCK_THAIID=false`.

## Production checklist

Serve over HTTPS and set `COOKIE_SECURE=true`, set a strong `ID_PEPPER` and `POSTGRES_PASSWORD`, set `APP_ORIGIN` to the public URL, set `MOCK_THAIID=false`, back up the database.
