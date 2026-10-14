package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/microcosm-cc/bluemonday"
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
	ID         string `json:"id"`
	Label      string `json:"label"`
	QuestionID string `json:"question_id,omitempty"`
	Count      int    `json:"count"` // 0 unless results_visible
}

type questionOut struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type pollOut struct {
	ID                string        `json:"id"`
	Title             string        `json:"title"`
	Description       string        `json:"description"`
	Kind              string        `json:"kind"`
	MaxChoices        int           `json:"max_choices"`
	Anonymous         bool          `json:"anonymous"`
	ResultsVisibility string        `json:"results_visibility"`
	StartsAt          time.Time     `json:"starts_at"`
	EndsAt            time.Time     `json:"ends_at"`
	PublishedAt       *time.Time    `json:"published_at"`
	ClosedAt          *time.Time    `json:"closed_at"`
	Status            string        `json:"status"` // draft | upcoming | open | closed
	AllowComment      bool          `json:"allow_comment"`
	Questions         []questionOut `json:"questions"`
	Options           []optionOut   `json:"options"`
	ImageURL          string        `json:"image_url"`
	ResultsVisible    bool          `json:"results_visible"`
	HasVoted          bool          `json:"has_voted"`
	BallotCount       int           `json:"ballot_count"`
	imageVer          int64
}

// resultsVisible reports whether voters may see tallies for a poll in this state.
func resultsVisible(status, visibility string) bool {
	return status == "closed" || (status == "open" && visibility == "live")
}

// statusSQL derives status from timestamps using the DB clock (single source of time).
const statusSQL = `CASE WHEN p.published_at IS NULL THEN 'draft'
	WHEN p.closed_at IS NOT NULL OR now() >= p.ends_at THEN 'closed'
	WHEN now() < p.starts_at THEN 'upcoming' ELSE 'open' END`

const pollCols = `p.id::text, p.title, p.description, p.kind, p.max_choices, p.anonymous, p.results_visibility,
	p.starts_at, p.ends_at, p.published_at, p.closed_at, ` + statusSQL + `,
	(SELECT count(*) FROM ballots b WHERE b.poll_id = p.id), p.allow_comment,
	coalesce((SELECT extract(epoch FROM i.updated_at)::bigint FROM poll_images i WHERE i.poll_id = p.id), 0)`

// loadPolls runs `SELECT pollCols FROM polls p <where>` and attaches questions, options (with
// tallies, only when the viewer may see results) and has_voted. This is the single gate for list/detail counts.
func (a *app) loadPolls(ctx context.Context, where string, voterID string, admin bool, args ...any) ([]pollOut, error) {
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
			&p.StartsAt, &p.EndsAt, &p.PublishedAt, &p.ClosedAt, &p.Status, &p.BallotCount, &p.AllowComment, &p.imageVer); err != nil {
			return nil, err
		}
		p.ResultsVisible = admin || resultsVisible(p.Status, p.ResultsVisibility)
		if p.imageVer > 0 {
			p.ImageURL = fmt.Sprintf("/api/polls/%s/image?v=%d", p.ID, p.imageVer)
		}
		p.Questions = []questionOut{}
		p.Options = []optionOut{}
		idx[p.ID] = len(polls)
		ids = append(ids, p.ID)
		polls = append(polls, p)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return polls, err
	}
	qrows, err := a.db.Query(ctx, `SELECT id::text, poll_id::text, label FROM poll_questions WHERE poll_id = ANY($1::uuid[]) ORDER BY position`, ids)
	if err != nil {
		return nil, err
	}
	defer qrows.Close()
	for qrows.Next() {
		var q questionOut
		var pid string
		if err := qrows.Scan(&q.ID, &pid, &q.Label); err != nil {
			return nil, err
		}
		polls[idx[pid]].Questions = append(polls[idx[pid]].Questions, q)
	}
	if err := qrows.Err(); err != nil {
		return nil, err
	}
	orows, err := a.db.Query(ctx, `SELECT o.id::text, o.poll_id::text, o.label, coalesce(o.question_id::text, ''), count(bc.ballot_id)
		FROM poll_options o LEFT JOIN ballot_choices bc ON bc.option_id = o.id
		WHERE o.poll_id = ANY($1::uuid[]) GROUP BY o.id ORDER BY o.position`, ids)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o optionOut
		var pid string
		if err := orows.Scan(&o.ID, &pid, &o.Label, &o.QuestionID, &o.Count); err != nil {
			return nil, err
		}
		if !polls[idx[pid]].ResultsVisible {
			o.Count = 0
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

const orderSQL = ` ORDER BY CASE WHEN ` + statusSQL + ` = 'open' THEN 0 WHEN ` + statusSQL + ` = 'upcoming' THEN 1 WHEN ` + statusSQL + ` = 'draft' THEN 2 ELSE 3 END, p.ends_at DESC, p.id`

// ---- voter-facing ----

type pollPage struct {
	Items   []pollOut      `json:"items"`
	Total   int            `json:"total"` // matching the status filter
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
	Counts  map[string]int `json:"counts"` // per status, ignoring the status filter
}

// listPage serves a paginated poll list. base is the visibility WHERE clause;
// query params: status (optional), page (from 1), per_page (default 10, max 50).
func (a *app) listPage(r *http.Request, base, voterID string, admin bool) (pollPage, error) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per < 1 || per > 50 {
		per = 10
	}
	rows, err := a.db.Query(r.Context(), `SELECT `+statusSQL+`, count(*) FROM polls p `+base+` GROUP BY 1`)
	if err != nil {
		return pollPage{}, err
	}
	defer rows.Close()
	counts := map[string]int{"draft": 0, "upcoming": 0, "open": 0, "closed": 0}
	total := 0
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return pollPage{}, err
		}
		counts[s] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return pollPage{}, err
	}
	where, args := base, []any{}
	if s := q.Get("status"); s != "" {
		n, ok := counts[s]
		if !ok {
			return pollPage{}, bad("invalid status")
		}
		where += ` AND ` + statusSQL + ` = $1`
		args = append(args, s)
		total = n
	}
	where += fmt.Sprintf("%s LIMIT %d OFFSET %d", orderSQL, per, (page-1)*per)
	items, err := a.loadPolls(r.Context(), where, voterID, admin, args...)
	if err != nil {
		return pollPage{}, err
	}
	return pollPage{items, total, page, per, counts}, nil
}

func (a *app) handleListPolls(w http.ResponseWriter, r *http.Request) error {
	out, err := a.listPage(r, `WHERE p.published_at IS NOT NULL`, a.voterID(r), false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *app) publishedPoll(r *http.Request) (pollOut, error) {
	id, err := pathID(r)
	if err != nil {
		return pollOut{}, err
	}
	polls, err := a.loadPolls(r.Context(), `WHERE p.id=$1 AND p.published_at IS NOT NULL`, a.voterID(r), false, id)
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
	AllowComment      bool      `json:"allow_comment"`
	Questions         []string  `json:"questions"`
	Options           []string  `json:"options"`
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// descPolicy limits the admin's rich-text description to basic formatting and links.
// No images or attachments: files can't be embedded, only linked.
var descPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "h2", "h3", "strong", "em", "u", "s", "ul", "ol", "li", "blockquote")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

const maxDescriptionBytes = 50_000

var approveLabels = []string{"เห็นด้วย", "ไม่เห็นด้วย", "งดออกเสียง"}

// validatePoll normalises in-place and returns the option labels to store.
func validatePoll(in *pollIn) ([]string, error) {
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Description) > 4*maxDescriptionBytes {
		return nil, bad("description too long")
	}
	in.Description = strings.TrimSpace(descPolicy.Sanitize(in.Description))
	if n := runes(in.Title); n < 1 || n > 200 {
		return nil, bad("title must be 1-200 characters")
	}
	if len(in.Description) > maxDescriptionBytes {
		return nil, bad("description too long")
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
		qs := make([]string, 0, len(in.Questions))
		for _, q := range in.Questions {
			q = strings.TrimSpace(q)
			if q == "" || runes(q) > 500 {
				return nil, bad("each question must be 1-500 characters")
			}
			qs = append(qs, q)
		}
		if len(qs) > 50 {
			return nil, bad("at most 50 questions")
		}
		if len(qs) == 0 {
			qs = []string{""} // single implicit question: the poll title is the question
		}
		in.Questions, in.MaxChoices = qs, len(qs)
	case "comment":
		in.MaxChoices = 1
	default:
		return nil, bad("kind must be single, multi, approve or comment")
	}
	if in.Kind != "approve" {
		in.Questions = nil
	}
	if in.Kind == "comment" {
		in.AllowComment = false // the comment is already the whole ballot
	}
	return labels, nil
}

// insertChoices stores a poll's options. For 'approve' polls each question gets
// its own agree / disagree / abstain options.
func insertChoices(ctx context.Context, tx pgx.Tx, pollID string, in pollIn, labels []string) error {
	pos := 0
	add := func(questionID any, label string) error {
		_, err := tx.Exec(ctx, `INSERT INTO poll_options (poll_id, question_id, label, position) VALUES ($1,$2,$3,$4)`, pollID, questionID, label, pos)
		pos++
		return err
	}
	for i, q := range in.Questions {
		var qid string
		if err := tx.QueryRow(ctx, `INSERT INTO poll_questions (poll_id, label, position) VALUES ($1,$2,$3) RETURNING id::text`, pollID, q, i).Scan(&qid); err != nil {
			return err
		}
		for _, l := range approveLabels {
			if err := add(qid, l); err != nil {
				return err
			}
		}
	}
	for _, l := range labels {
		if err := add(nil, l); err != nil {
			return err
		}
	}
	return nil
}

// ---- admin handlers ----

func (a *app) handleAdminListPolls(w http.ResponseWriter, r *http.Request, _ string) error {
	out, err := a.listPage(r, `WHERE true`, "", true)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *app) handleAdminGetPoll(w http.ResponseWriter, r *http.Request, _ string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	polls, err := a.loadPolls(r.Context(), `WHERE p.id=$1`, "", true, id)
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
			INSERT INTO polls (title, description, kind, max_choices, anonymous, results_visibility, starts_at, ends_at, allow_comment, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text`,
			in.Title, in.Description, in.Kind, in.MaxChoices, in.Anonymous, in.ResultsVisibility, in.StartsAt, in.EndsAt, in.AllowComment, adminID).Scan(&id); err != nil {
			return err
		}
		return insertChoices(r.Context(), tx, id, in, labels)
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
			  results_visibility=$7, starts_at=$8, ends_at=$9, allow_comment=$10, updated_at=now() WHERE id=$1`,
			id, in.Title, in.Description, in.Kind, in.MaxChoices, in.Anonymous, in.ResultsVisibility, in.StartsAt, in.EndsAt, in.AllowComment); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM poll_options WHERE poll_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM poll_questions WHERE poll_id=$1`, id); err != nil {
			return err
		}
		return insertChoices(r.Context(), tx, id, in, labels)
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
