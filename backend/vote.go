package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var genesisHash = make([]byte, 32)

// chainHash links a ballot to the previous one. Fields are length-prefixed so that
// different (choices, comment) splits can never produce the same input.
func chainHash(prev []byte, pollID string, seq int64, voterID string, optionIDs []string, comment string, receiptHash []byte) []byte {
	ids := append([]string(nil), optionIDs...)
	sort.Strings(ids)
	var buf bytes.Buffer
	put := func(b []byte) {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(b)))
		buf.Write(b)
	}
	put(prev)
	put([]byte(pollID))
	_ = binary.Write(&buf, binary.BigEndian, seq)
	put([]byte(voterID))
	put([]byte(strings.Join(ids, ",")))
	put([]byte(comment))
	put(receiptHash)
	s := sha256.Sum256(buf.Bytes())
	return s[:]
}

// ---- receipts ----

const receiptAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I

// newReceipt returns a 26-char code (130 bits) formatted XXXXX-XXXXX-... and its hash.
func newReceipt() (code string, hash []byte, err error) {
	raw := make([]byte, 26)
	for i := range raw {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(receiptAlphabet))))
		if err != nil {
			return "", nil, err
		}
		raw[i] = receiptAlphabet[n.Int64()]
	}
	return formatReceipt(string(raw)), sha(raw), nil
}

func formatReceipt(raw string) string {
	var parts []string
	for i := 0; i < len(raw); i += 5 {
		end := i + 5
		if end > len(raw) {
			end = len(raw)
		}
		parts = append(parts, raw[i:end])
	}
	return strings.Join(parts, "-")
}

var receiptRe = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{26}$`)

func normalizeReceipt(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	return s, receiptRe.MatchString(s)
}

// ---- cast vote ----

func (a *app) handleVote(w http.ResponseWriter, r *http.Request) error {
	voter := a.voterID(r)
	if voter == "" {
		return unauth("login required")
	}
	pollID, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		OptionIDs []string `json:"option_ids"`
		Comment   string   `json:"comment"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Comment = strings.TrimSpace(in.Comment)
	code, rhash, err := newReceipt()
	if err != nil {
		return err
	}

	ctx := r.Context()
	err = a.withTx(ctx, func(tx pgx.Tx) error {
		// Lock the poll row: serializes votes per poll so seq/prev_hash stay consistent.
		var kind, status string
		var maxChoices int
		var anonymous, allowComment bool
		err := tx.QueryRow(ctx, `SELECT p.kind, p.max_choices, p.anonymous, p.allow_comment, `+statusSQL+`
			FROM polls p WHERE p.id=$1 AND p.published_at IS NOT NULL FOR UPDATE`, pollID).Scan(&kind, &maxChoices, &anonymous, &allowComment, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("poll not found")
		}
		if err != nil {
			return err
		}
		switch status {
		case "upcoming":
			return conflict("poll has not started yet")
		case "closed":
			return conflict("poll is closed")
		}

		// choice rules
		if kind == "comment" {
			if len(in.OptionIDs) != 0 {
				return bad("this poll takes a comment only")
			}
			if n := runes(in.Comment); n < 1 || n > 2000 {
				return bad("comment must be 1-2000 characters")
			}
		} else {
			if in.Comment != "" && !allowComment {
				return bad("this poll does not accept a comment")
			}
			if runes(in.Comment) > 2000 {
				return bad("comment must be at most 2000 characters")
			}
			if kind == "approve" {
				// maxChoices is the number of questions: every one must be answered exactly once
				if len(in.OptionIDs) != maxChoices {
					return bad(fmt.Sprintf("answer all %d questions", maxChoices))
				}
			} else if len(in.OptionIDs) < 1 || len(in.OptionIDs) > maxChoices {
				return bad(fmt.Sprintf("choose between 1 and %d options", maxChoices))
			}
			seen := map[string]bool{}
			for _, o := range in.OptionIDs {
				o = strings.ToLower(o)
				if !uuidRe.MatchString(o) || seen[o] {
					return bad("invalid or duplicate option")
				}
				seen[o] = true
			}
			var valid int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM poll_options WHERE poll_id=$1 AND id = ANY($2::uuid[])`,
				pollID, in.OptionIDs).Scan(&valid); err != nil {
				return err
			}
			if valid != len(in.OptionIDs) {
				return bad("option does not belong to this poll")
			}
			if kind == "approve" {
				var answered int
				if err := tx.QueryRow(ctx, `SELECT count(DISTINCT question_id) FROM poll_options WHERE poll_id=$1 AND id = ANY($2::uuid[])`,
					pollID, in.OptionIDs).Scan(&answered); err != nil {
					return err
				}
				if answered != maxChoices {
					return bad("answer each question exactly once")
				}
			}
		}

		// one person, one vote: enforced by the (poll_id, voter_id) primary key.
		// Anonymous polls store no timestamps so participation can't be matched to a ballot by time.
		_, err = tx.Exec(ctx, `INSERT INTO participations (poll_id, voter_id, created_at)
			VALUES ($1,$2, CASE WHEN $3 THEN NULL ELSE now() END)`, pollID, voter, anonymous)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return conflict("you have already voted in this poll")
		}
		if err != nil {
			return err
		}

		prev, seq := genesisHash, int64(1)
		var last []byte
		var lastSeq int64
		err = tx.QueryRow(ctx, `SELECT seq, hash FROM ballots WHERE poll_id=$1 ORDER BY seq DESC LIMIT 1`, pollID).Scan(&lastSeq, &last)
		if err == nil {
			prev, seq = last, lastSeq+1
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		ids := make([]string, len(in.OptionIDs))
		for i, o := range in.OptionIDs {
			ids[i] = strings.ToLower(o)
		}
		storedVoter := voter
		if anonymous {
			storedVoter = ""
		}
		hash := chainHash(prev, pollID, seq, storedVoter, ids, in.Comment, rhash)

		var vArg any = storedVoter
		if anonymous {
			vArg = nil
		}
		var cArg any = in.Comment
		if in.Comment == "" {
			cArg = nil
		}
		var ballotID string
		if err := tx.QueryRow(ctx, `INSERT INTO ballots (poll_id, seq, voter_id, comment, receipt_hash, prev_hash, hash, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, CASE WHEN $8 THEN NULL ELSE now() END) RETURNING id::text`,
			pollID, seq, vArg, cArg, rhash, prev, hash, anonymous).Scan(&ballotID); err != nil {
			return err
		}
		for _, o := range ids {
			if _, err := tx.Exec(ctx, `INSERT INTO ballot_choices (ballot_id, option_id) VALUES ($1,$2)`, ballotID, o); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]string{"receipt": code})
	return nil
}

func (a *app) handleReceipt(w http.ResponseWriter, r *http.Request) error {
	raw, ok := normalizeReceipt(r.PathValue("code"))
	if !ok {
		return notFound("receipt not found")
	}
	var title, pollID string
	var seq int64
	var hash []byte
	err := a.db.QueryRow(r.Context(), `SELECT p.id::text, p.title, b.seq, b.hash
		FROM ballots b JOIN polls p ON p.id=b.poll_id WHERE b.receipt_hash=$1`, sha([]byte(raw))).Scan(&pollID, &title, &seq, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("receipt not found")
	}
	if err != nil {
		return err
	}
	// Deliberately does not reveal what was chosen (prevents proving a vote to a buyer).
	writeJSON(w, http.StatusOK, map[string]any{"poll_id": pollID, "poll_title": title, "seq": seq, "hash": hex.EncodeToString(hash)})
	return nil
}

// ---- results ----

type optionResult struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	QuestionID string `json:"question_id,omitempty"`
	Count      int    `json:"count"`
}

type ballotOut struct {
	ID      string   `json:"id,omitempty"` // admin only
	Name    *string  `json:"name"`         // public (non-anonymous) polls only
	Choices []string `json:"choices"`
	Comment *string  `json:"comment"`
	Hidden  bool     `json:"hidden"`
	Reason  string   `json:"hide_reason,omitempty"`
}

type resultsOut struct {
	Poll     pollOut        `json:"poll"`
	Total    int            `json:"total"`
	Options  []optionResult `json:"options"`
	Ballots  []ballotOut    `json:"ballots"`
	HeadHash string         `json:"head_hash"`
}

func (a *app) buildResults(ctx context.Context, p pollOut, admin bool) (resultsOut, error) {
	out := resultsOut{Poll: p, Total: p.BallotCount, Options: []optionResult{}, Ballots: []ballotOut{}}

	orows, err := a.db.Query(ctx, `SELECT o.id::text, o.label, coalesce(o.question_id::text, ''), count(bc.ballot_id)
		FROM poll_options o LEFT JOIN ballot_choices bc ON bc.option_id=o.id
		WHERE o.poll_id=$1 GROUP BY o.id ORDER BY o.position`, p.ID)
	if err != nil {
		return out, err
	}
	defer orows.Close()
	for orows.Next() {
		var o optionResult
		if err := orows.Scan(&o.ID, &o.Label, &o.QuestionID, &o.Count); err != nil {
			return out, err
		}
		out.Options = append(out.Options, o)
	}
	if err := orows.Err(); err != nil {
		return out, err
	}

	// ponytail: loads every ballot; paginate if polls reach tens of thousands of ballots.
	brows, err := a.db.Query(ctx, `
		SELECT b.id::text, v.display_name, b.comment,
		       coalesce(array_agg(CASE WHEN q.label <> '' THEN q.label || ': ' || o.label ELSE o.label END ORDER BY o.position)
		                FILTER (WHERE o.id IS NOT NULL), '{}'),
		       h.ballot_id IS NOT NULL, coalesce(h.reason, '')
		FROM ballots b
		LEFT JOIN voters v ON v.id=b.voter_id
		LEFT JOIN ballot_choices bc ON bc.ballot_id=b.id
		LEFT JOIN poll_options o ON o.id=bc.option_id
		LEFT JOIN poll_questions q ON q.id=o.question_id
		LEFT JOIN comment_hidden h ON h.ballot_id=b.id
		WHERE b.poll_id=$1
		GROUP BY b.id, b.seq, v.display_name, b.comment, h.ballot_id, h.reason
		ORDER BY b.seq`, p.ID)
	if err != nil {
		return out, err
	}
	defer brows.Close()
	for brows.Next() {
		var b ballotOut
		if err := brows.Scan(&b.ID, &b.Name, &b.Comment, &b.Choices, &b.Hidden, &b.Reason); err != nil {
			return out, err
		}
		if p.Anonymous {
			// anonymous: only comments are listed, with no link to a choice or voter
			if b.Comment == nil {
				continue
			}
			b.Choices = []string{}
		}
		if !admin {
			b.ID, b.Reason = "", ""
			if b.Hidden {
				if p.Anonymous {
					continue
				}
				b.Comment = nil
			}
		}
		out.Ballots = append(out.Ballots, b)
	}
	if err := brows.Err(); err != nil {
		return out, err
	}
	if p.Anonymous { // remove vote-order information
		for i := len(out.Ballots) - 1; i > 0; i-- {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
			if err != nil {
				return out, err
			}
			j := int(n.Int64())
			out.Ballots[i], out.Ballots[j] = out.Ballots[j], out.Ballots[i]
		}
	}

	var head []byte
	err = a.db.QueryRow(ctx, `SELECT hash FROM ballots WHERE poll_id=$1 ORDER BY seq DESC LIMIT 1`, p.ID).Scan(&head)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out.HeadHash = hex.EncodeToString(head)
	return out, nil
}

func (a *app) handlePublicResults(w http.ResponseWriter, r *http.Request) error {
	p, err := a.publishedPoll(r)
	if err != nil {
		return err
	}
	if !p.ResultsVisible {
		return forbid("results are not available yet")
	}
	res, err := a.buildResults(r.Context(), p, false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (a *app) handleAdminResults(w http.ResponseWriter, r *http.Request, _ string) error {
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
	res, err := a.buildResults(r.Context(), polls[0], true)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

// ---- admin: verify chain, hide comment ----

func (a *app) handleAdminVerify(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM polls WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return notFound("poll not found")
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT b.seq, coalesce(b.voter_id::text, ''), coalesce(b.comment, ''), b.receipt_hash, b.prev_hash, b.hash,
		       coalesce(array_agg(bc.option_id::text) FILTER (WHERE bc.option_id IS NOT NULL), '{}')
		FROM ballots b LEFT JOIN ballot_choices bc ON bc.ballot_id=b.id
		WHERE b.poll_id=$1 GROUP BY b.id ORDER BY b.seq`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	problems := []string{}
	prev, expect, n := genesisHash, int64(1), 0
	for rows.Next() {
		var seq int64
		var voter, comment string
		var rh, ph, hash []byte
		var opts []string
		if err := rows.Scan(&seq, &voter, &comment, &rh, &ph, &hash, &opts); err != nil {
			return err
		}
		n++
		if seq != expect {
			problems = append(problems, fmt.Sprintf("sequence gap: expected #%d, found #%d", expect, seq))
			expect = seq
		}
		if !bytes.Equal(ph, prev) {
			problems = append(problems, fmt.Sprintf("ballot #%d: prev_hash does not match previous ballot", seq))
		}
		if !bytes.Equal(chainHash(ph, id, seq, voter, opts, comment, rh), hash) {
			problems = append(problems, fmt.Sprintf("ballot #%d: content does not match its hash", seq))
		}
		prev, expect = hash, expect+1
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var parts int
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM participations WHERE poll_id=$1`, id).Scan(&parts); err != nil {
		return err
	}
	if parts != n {
		problems = append(problems, fmt.Sprintf("participations (%d) != ballots (%d)", parts, n))
	}
	a.audit(r.Context(), adminID, "poll.verify", id, map[string]any{"ok": len(problems) == 0, "ballots": n})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": len(problems) == 0, "ballots": n, "participations": parts,
		"head_hash": hex.EncodeToString(prev), "problems": problems, "checked_at": time.Now().UTC(),
	})
	return nil
}

func (a *app) handleAdminHideComment(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if runes(in.Reason) > 500 {
		return bad("reason too long")
	}
	tag, err := a.db.Exec(r.Context(), `INSERT INTO comment_hidden (ballot_id, admin_id, reason)
		SELECT id, $2, $3 FROM ballots WHERE id=$1 AND comment IS NOT NULL ON CONFLICT DO NOTHING`, id, adminID, in.Reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return conflict("ballot has no comment or is already hidden")
	}
	a.audit(r.Context(), adminID, "comment.hide", id, map[string]string{"reason": in.Reason})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
