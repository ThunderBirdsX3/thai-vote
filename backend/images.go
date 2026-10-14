package main

import (
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const maxImageBytes = 2 << 20

var imageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// lockEditable locks the poll row and fails unless it can still be edited (draft or upcoming).
func lockEditable(r *http.Request, tx pgx.Tx, id string) error {
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
	return nil
}

func (a *app) handleAdminSetImage(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageBytes))
	if err != nil {
		return bad("image too large (max 2 MB)")
	}
	ct := http.DetectContentType(data) // sniffed: the client's Content-Type is not trusted
	if !imageTypes[ct] {
		return bad("image must be JPEG, PNG or WebP")
	}
	err = a.withTx(r.Context(), func(tx pgx.Tx) error {
		if err := lockEditable(r, tx, id); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO poll_images (poll_id, content_type, data) VALUES ($1,$2,$3)
			ON CONFLICT (poll_id) DO UPDATE SET content_type=$2, data=$3, updated_at=now()`, id, ct, data)
		return err
	})
	if err != nil {
		return err
	}
	a.audit(r.Context(), adminID, "poll.image", id, map[string]int{"bytes": len(data)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (a *app) handleAdminDeleteImage(w http.ResponseWriter, r *http.Request, adminID string) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	err = a.withTx(r.Context(), func(tx pgx.Tx) error {
		if err := lockEditable(r, tx, id); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `DELETE FROM poll_images WHERE poll_id=$1`, id)
		return err
	})
	if err != nil {
		return err
	}
	a.audit(r.Context(), adminID, "poll.image.delete", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// handleImage serves a promo image. Unpublished polls are visible to admins only.
func (a *app) handleImage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var ct string
	var data []byte
	err = a.db.QueryRow(r.Context(), `SELECT i.content_type, i.data FROM poll_images i JOIN polls p ON p.id=i.poll_id
		WHERE i.poll_id=$1 AND (p.published_at IS NOT NULL OR $2)`, id, a.adminID(r) != "").Scan(&ct, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("image not found")
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // URL carries ?v=<updated_at>
	_, _ = w.Write(data)
	return nil
}
