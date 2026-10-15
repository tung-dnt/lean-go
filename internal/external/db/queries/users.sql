-- name: GetUser :one
SELECT * FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = LOWER(sqlc.arg(email)::text);

-- name: CreateUser :one
INSERT INTO users (name, email, password, verified, admin)
VALUES (
    sqlc.arg(name)::text,
    LOWER(sqlc.arg(email)::text),
    sqlc.arg(password)::text,
    sqlc.arg(verified)::boolean,
    sqlc.arg(admin)::boolean
)
RETURNING *;
