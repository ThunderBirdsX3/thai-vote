-- +goose Up
ALTER TABLE polls ADD COLUMN allow_comment boolean NOT NULL DEFAULT false;

-- Statements of an 'approve' poll; each gets its own agree/disagree/abstain options.
CREATE TABLE poll_questions (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id  uuid NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    label    text NOT NULL,
    position int  NOT NULL,
    UNIQUE (poll_id, position)
);

ALTER TABLE poll_options ADD COLUMN question_id uuid REFERENCES poll_questions(id) ON DELETE CASCADE;

INSERT INTO poll_questions (poll_id, label, position) SELECT id, '', 0 FROM polls WHERE kind = 'approve';
UPDATE poll_options o SET question_id = q.id FROM poll_questions q WHERE q.poll_id = o.poll_id;

CREATE TABLE poll_images (
    poll_id      uuid PRIMARY KEY REFERENCES polls(id) ON DELETE CASCADE,
    content_type text  NOT NULL,
    data         bytea NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Descriptions are now sanitized HTML; convert existing plain text.
UPDATE polls SET description = '<p>' || replace(replace(replace(replace(description,
    '&', '&amp;'), '<', '&lt;'), '>', '&gt;'), E'\n', '<br>') || '</p>'
WHERE description <> '';

-- +goose Down
DROP TABLE poll_images;
ALTER TABLE poll_options DROP COLUMN question_id;
DROP TABLE poll_questions;
ALTER TABLE polls DROP COLUMN allow_comment;
