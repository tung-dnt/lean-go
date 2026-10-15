CREATE TABLE notes (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    body       TEXT NOT NULL CHECK (length(body) <= 5000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX notes_created_at_idx ON notes (created_at DESC, id DESC);
