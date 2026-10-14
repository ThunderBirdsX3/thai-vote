package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func pathID(r *http.Request) (string, error) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidRe.MatchString(id) {
		return "", notFound("not found")
	}
	return id, nil
}

func (a *app) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, a.db, fn)
}

func (a *app) audit(ctx context.Context, adminID, action, target string, detail any) {
	b, _ := json.Marshal(detail)
	if detail == nil {
		b = []byte("{}")
	}
	var aid any = adminID
	if adminID == "" {
		aid = nil
	}
	_, err := a.db.Exec(ctx, `INSERT INTO audit_log (admin_id, action, target, detail) VALUES ($1,$2,$3,$4)`, aid, action, target, b)
	if err != nil {
		// audit failure must be visible, but must not break the admin's request
		println("audit log error:", err.Error())
	}
}

// ---- poll model ----

type optionOut struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type pollOut struct {
	ID                string      `json:"id"`
	Title             string      `json:"title"`
	Description       string      `json:"description"`
	Kind              string      `json:"kind"`
	MaxChoices        int         `json:"max_choices"`
	Anonymous         bool        `json:"anonymous"`
	ResultsVisibility string      `json:"results_visibility"`
	StartsAt          time.Time   `json:"starts_at"`
	EndsAt            time.Time   `json:"ends_at"`
	PublishedAt       *time.Time  `json:"published_at"`
	ClosedAt          *time.Time  `json:"closed_at"`
	Status            string      `json:"status"` // draft | upcoming | open | closed
	Options           []optionOut `json:"options"`
	HasVoted          bool        `json:"has_voted"`
	BallotCount       int         `json:"ballot_count"`
}

// statusSQL derives status from timestamps using the DB clock (single source of time).
const statusSQL = `CASE WHEN p.published_at IS NULL THEN 'draft'
	WHEN p.closed_at IS NOT NULL OR now() >= p.ends_at THEN 'closed'
	WHEN now() < p.starts_at THEN 'upcoming' ELSE 'open' END`

const pollCols = `p.id::text, p.title, p.description, p.kind, p.max_choices, p.anonymous, p.results_visibility,
	p.starts_at, p.ends_at, p.published_at, p.closed_at, ` + statusSQL + `,
	(SELECT count(*) FROM ballots b WHERE b.poll_id = p.id)`

// loadPolls runs `SELECT pollCols FROM polls p <where>` and attaches options and has_voted.
func (a *app) loadPolls(ctx context.Context, where string, voterID string, args ...any) ([]pollOut, error) {
	rows, err := a.db.Query(ctx, `SELECT `+pollCols+` FROM polls p `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	polls := []pollOut{}
	idx := map[string]int{}
	var ids []string
	for rows.Next() {
		var p pollOut
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.Kind, &p.MaxChoices, &p.Anonymous, &p.ResultsVisibility,
			&p.StartsAt, &p.EndsAt, &p.PublishedAt, &p.ClosedAt, &p.Status, &p.BallotCount); err != nil {
			return nil, err
		}
		p.Options = []optionOut{}
		idx[p.ID] = len(polls)
		ids = append(ids, p.ID)
		polls = append(polls, p)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return polls, err
	}
	orows, err := a.db.Query(ctx, `SELECT id::text, poll_id::text, label FROM poll_options WHERE poll_id = ANY($1::uuid[]) ORDER BY position`, ids)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o optionOut
		var pid string
		if err := orows.Scan(&o.ID, &pid, &o.Label); err != nil {
			return nil, err
		}
		polls[idx[pid]].Options = append(polls[idx[pid]].Options, o)
	}
	if err := orows.Err(); err != nil {
		return nil, err
	}
	if voterID != "" {
		vrows, err := a.db.Query(ctx, `SELECT poll_id::text FROM participations WHERE voter_id=$1 AND poll_id = ANY($2::uuid[])`, voterID, ids)
		if err != nil {
			return nil, err
		}
		defer vrows.Close()
		for vrows.Next() {
			var pid string
			if err := vrows.Scan(&pid); err != nil {
				return nil, err
			}
			polls[idx[pid]].HasVoted = true
		}
		return polls, vrows.Err()
	}
	return polls, nil
}

const orderSQL = ` ORDER BY CASE WHEN ` + statusSQL + ` = 'open' THEN 0 WHEN ` + statusSQL + ` = 'upcoming' THEN 1 WHEN ` + statusSQL + ` = 'draft' THEN 2 ELSE 3 END, p.ends_at DESC`

// ---- voter-facing ----

func (a *app) handleListPolls(w http.ResponseWriter, r *http.Request) error {
	polls, err := a.loadPolls(r.Context(), `WHERE p.published_at IS NOT NULL`+orderSQL, a.voterID(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, polls)
	return nil
}

func (a *app) publishedPoll(r *http.Request) (pollOut, error) {
	id, err := pathID(r)
	if err != nil {
		return pollOut{}, err
	}
	polls, err := a.loadPolls(r.Context(), `WHERE p.id=$1 AND p.published_at IS NOT NULL`, a.voterID(r), id)
	if err != nil {
		return pollOut{}, err
	}
	if len(polls) == 0 {
		return pollOut{}, notFound("poll not found")
	}
	return polls[0], nil
}

func (a *app) handleGetPoll(w http.ResponseWriter, r *http.Request) error {
	p, err := a.publishedPoll(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

// ---- admin: validation ----

type pollIn struct {
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Kind              string    `json:"kind"`
	MaxChoices        int       `json:"max_choices"`
	Anonymous         bool      `json:"anonymous"`
	ResultsVisibility string    `json:"results_visibility"`
	StartsAt          time.Time `json:"starts_at"`
	EndsAt            time.Time `json:"ends_at"`
	Options           []string  `json:"options"`
}

func runes(s string) int { return utf8.RuneCountInString(s) }

var approveLabels = []string{"เห็นด้วย", "ไม่เห็นด้วย", "งดออกเสียง"}

// validatePoll normalises in-place and returns the option labels to store.
func validatePoll(in *pollIn) ([]string, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if n := runes(in.Title); n < 1 || n > 200 {
		return nil, bad("title must be 1-200 characters")
	}
	if runes(in.Description) > 5000 {
		return nil, bad("description too long (max 5000)")
	}
	if in.ResultsVisibility != "live" && in.ResultsVisibility != "after_close" {
		return nil, bad("results_visibility must be live or after_close")
	}
	if in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) {
		return nil, bad("ends_at must be after starts_at")
	}
	var labels []string
	switch in.Kind {
	case "single", "multi":
		for _, o := range in.Options {
			o = strings.TrimSpace(o)
			if n := runes(o); n < 1 || n > 200 {
				return nil, bad("each option must be 1-200 characters")
			}
			labels = append(labels, o)
		}
		if len(labels) < 2 || len(labels) > 20 {
			return nil, bad("need 2-20 options")
		}
		seen := map[string]bool{}
		for _, l := range labels {
			if seen[l] {
				return nil, bad("duplicate option: " + l)
			}
			seen[l] = true
		}
		if in.Kind == "single" {
			in.MaxChoices = 1
		} else if in.MaxChoices < 1 || in.MaxChoices > len(labels) {
			return nil, bad("max_choices must be between 1 and number of options")
		}
	case "approve":
		labels, in.MaxChoices = approveLabels, 1
	case "comment":
		in.MaxChoices = 1
	default:
		return nil, bad("kind must be single, multi, approve or comment")
	}
	return labels, nil
}

func insertOptions(ctx context.Context, tx pgx.Tx, pollID string, labels []string) error {
	for i, l := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO poll_options (poll_id, label, position) VALUES ($1,$2,$3)`, pollID, l, i); err != nil {
			return err
		}
	}
	return nil
}

// ---- admin handlers ----

func (a *app) handleAdminListPolls(w http.ResponseWriter, r *http.Request, _ string) error {
	polls, err := a.loadPolls(r.Context(), orderSQL, "")
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, polls)
	return nil
}

func (a *app) handleAdminGetPoll(w http.ResponseWriter, r *http.Request, _ string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	polls, err := a.loadPolls(r.Context(), `WHERE p.id=$1`, "", id)
	if err != nil {
		return err
	}
	if len(polls) == 0 {
		return notFound("poll not found")
	}
	writeJSON(w, http.StatusOK, polls[0])
	return nil
}

func (a *app) readPollIn(r *http.Request) (pollIn, []string, error) {
	var in pollIn
	if err := readJSON(r, &in); err != nil {
		return pollIn{}, nil, err
	}
	labels, err := validatePoll(&in)
	return in, labels, err
}

func (a *app) handleAdminCreatePoll(w http.ResponseWriter, r *http.Request, adminID string) error {
	in, labels, err := a.readPollIn(r)
	if err != nil {
		return err
	}
	var id string
	err = a.withTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO polls (title, description, kind, max_choices, anonymous, results_visibility, starts_at, ends_at, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text`,
			in.Title, in.Description, in.Kind, in.MaxChoices, in.Anonymous, in.ResultsVisibility, in.StartsAt, in.EndsAt, adminID).Scan(&id); err != nil {
			return err
		}
		return insertOptions(r.Context(), tx, id, labels)
	})
	if err != nil {
		return err
	}
	a.audit(r.Context(), adminID, "poll.create", id, map[string]string{"title": in.Title})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	return nil
}

func (a *app) handleAdminUpdatePoll(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	in, labels, err := a.readPollIn(r)
	if err != nil {
		return err
	}
	err = a.withTx(r.Context(), func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(r.Context(), `SELECT `+statusSQL+` FROM polls p WHERE p.id=$1 FOR UPDATE`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("poll not found")
		}
		if err != nil {
			return err
		}
		if status != "draft" && status != "upcoming" {
			return conflict("poll can only be edited before it starts")
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE polls SET title=$2, description=$3, kind=$4, max_choices=$5, anonymous=$6,
			  results_visibility=$7, starts_at=$8, ends_at=$9, updated_at=now() WHERE id=$1`,
			id, in.Title, in.Description, in.Kind, in.MaxChoices, in.Anonymous, in.ResultsVisibility, in.StartsAt, in.EndsAt); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM poll_options WHERE poll_id=$1`, id); err != nil {
			return err
		}
		return insertOptions(r.Context(), tx, id, labels)
	})
	if err != nil {
		return err
	}
	a.audit(r.Context(), adminID, "poll.update", id, map[string]string{"title": in.Title})
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
	return nil
}

func (a *app) handleAdminDeletePoll(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	// Only never-published drafts can be deleted: they cannot have ballots.
	tag, err := a.db.Exec(r.Context(), `DELETE FROM polls WHERE id=$1 AND published_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return conflict("only unpublished drafts can be deleted")
	}
	a.audit(r.Context(), adminID, "poll.delete", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleAdminPublish(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE polls SET published_at=now(), updated_at=now() WHERE id=$1 AND published_at IS NULL AND ends_at > now()`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return conflict("poll is already published or its end time has passed")
	}
	a.audit(r.Context(), adminID, "poll.publish", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleAdminClose(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE polls SET closed_at=now(), updated_at=now()
		WHERE id=$1 AND published_at IS NOT NULL AND closed_at IS NULL AND ends_at > now() AND starts_at <= now()`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return conflict("only an open poll can be closed early")
	}
	a.audit(r.Context(), adminID, "poll.close", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleAdminAudit(w http.ResponseWriter, r *http.Request, _ string) error {
	rows, err := a.db.Query(r.Context(), `
		SELECT l.id, l.action, l.target, l.detail, l.created_at, coalesce(ad.username::text, '')
		FROM audit_log l LEFT JOIN admins ad ON ad.id = l.admin_id ORDER BY l.id DESC LIMIT 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type entry struct {
		ID        int64           `json:"id"`
		Action    string          `json:"action"`
		Target    string          `json:"target"`
		Detail    json.RawMessage `json:"detail"`
		CreatedAt time.Time       `json:"created_at"`
		Admin     string          `json:"admin"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Action, &e.Target, &e.Detail, &e.CreatedAt, &e.Admin); err != nil {
			return err
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, out)
	return rows.Err()
}
