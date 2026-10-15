-- name: CreateNote :one
INSERT INTO notes (title, body)
VALUES (sqlc.arg(title)::text, sqlc.arg(body)::text)
RETURNING *;

-- name: GetNote :one
SELECT * FROM notes
WHERE id = $1;

-- name: CountNotes :one
SELECT COUNT(*) FROM notes;

-- name: ListNotes :many
-- Newest first; id breaks the tie of notes made in the same instant, so a page never repeats or skips one.
SELECT * FROM notes
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: DeleteNote :execrows
DELETE FROM notes
WHERE id = $1;
