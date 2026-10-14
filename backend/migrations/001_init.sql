-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE admins (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username      citext NOT NULL UNIQUE,
    password_hash text NOT NULL,
    display_name  text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);

CREATE TABLE voters (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    citizen_id_hash   bytea NOT NULL UNIQUE,
    citizen_id_masked text NOT NULL,
    display_name      text NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_login_at     timestamptz
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    kind       text NOT NULL CHECK (kind IN ('admin', 'voter')),
    subject_id uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE polls (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title              text NOT NULL,
    description        text NOT NULL DEFAULT '',
    kind               text NOT NULL CHECK (kind IN ('single', 'multi', 'approve', 'comment')),
    max_choices        int  NOT NULL DEFAULT 1 CHECK (max_choices >= 1),
    anonymous          boolean NOT NULL,
    results_visibility text NOT NULL CHECK (results_visibility IN ('live', 'after_close')),
    starts_at          timestamptz NOT NULL,
    ends_at            timestamptz NOT NULL,
    published_at       timestamptz,
    closed_at          timestamptz,
    created_by         uuid NOT NULL REFERENCES admins(id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE TABLE poll_options (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id  uuid NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    label    text NOT NULL,
    position int  NOT NULL,
    UNIQUE (poll_id, position)
);

-- Who has voted. Separate from ballots so anonymous ballots cannot be linked to voters.
CREATE TABLE participations (
    poll_id    uuid NOT NULL REFERENCES polls(id),
    voter_id   uuid NOT NULL REFERENCES voters(id),
    created_at timestamptz, -- NULL for anonymous polls (no timing correlation with ballots)
    PRIMARY KEY (poll_id, voter_id)
);

CREATE TABLE ballots (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id      uuid NOT NULL REFERENCES polls(id),
    seq          bigint NOT NULL,
    voter_id     uuid REFERENCES voters(id), -- NULL for anonymous polls
    comment      text,
    receipt_hash bytea NOT NULL UNIQUE,
    prev_hash    bytea NOT NULL,
    hash         bytea NOT NULL,
    created_at   timestamptz, -- NULL for anonymous polls
    UNIQUE (poll_id, seq)
);

CREATE TABLE ballot_choices (
    ballot_id uuid NOT NULL REFERENCES ballots(id),
    option_id uuid NOT NULL REFERENCES poll_options(id),
    PRIMARY KEY (ballot_id, option_id)
);

CREATE TABLE comment_hidden (
    ballot_id  uuid PRIMARY KEY REFERENCES ballots(id),
    admin_id   uuid NOT NULL REFERENCES admins(id),
    reason     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id         bigserial PRIMARY KEY,
    admin_id   uuid REFERENCES admins(id),
    action     text NOT NULL,
    target     text NOT NULL DEFAULT '',
    detail     jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Cast votes are append-only.
-- +goose StatementBegin
CREATE FUNCTION reject_modification() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% on % is not allowed: votes are append-only', TG_OP, TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER ballots_append_only BEFORE UPDATE OR DELETE ON ballots
    FOR EACH ROW EXECUTE FUNCTION reject_modification();
CREATE TRIGGER ballot_choices_append_only BEFORE UPDATE OR DELETE ON ballot_choices
    FOR EACH ROW EXECUTE FUNCTION reject_modification();
CREATE TRIGGER participations_append_only BEFORE UPDATE OR DELETE ON participations
    FOR EACH ROW EXECUTE FUNCTION reject_modification();

-- +goose Down
DROP TABLE audit_log, comment_hidden, ballot_choices, ballots, participations,
    poll_options, polls, sessions, voters, admins;
DROP FUNCTION reject_modification();
