-- name: CreateUser :one
INSERT INTO users (id, email)
VALUES ($1, $2)
RETURNING id, email, created_at;

-- name: GetUser :one
SELECT id, email, created_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, created_at
FROM users
WHERE email = $1;

-- name: CreateUserImport :one
INSERT INTO user_imports (id, total_count, correlation_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateUserImportEntry :exec
INSERT INTO user_import_entries (import_id, user_id, email)
VALUES ($1, $2, $3);

-- name: GetUserImport :one
SELECT *
FROM user_imports
WHERE id = $1;

-- name: StartUserImport :one
UPDATE user_imports
SET state = 'running', started_at = COALESCE(started_at, now())
WHERE id = $1 AND state IN ('pending', 'running')
RETURNING id;

-- name: ListPendingUserImportEntries :many
SELECT entries.import_id, entries.user_id, entries.email, entries.state, imports.correlation_id
FROM user_import_entries AS entries
JOIN user_imports AS imports ON imports.id = entries.import_id
WHERE entries.import_id = $1 AND entries.state = 'pending'
ORDER BY email;

-- name: CreateImportedUser :one
INSERT INTO users (id, email)
VALUES ($1, $2)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: CompleteUserImportEntry :exec
UPDATE user_import_entries
SET state = 'completed'
WHERE import_id = $1 AND user_id = $2 AND state = 'pending';

-- name: FailUserImportEntry :exec
UPDATE user_import_entries
SET state = 'failed'
WHERE import_id = $1 AND user_id = $2 AND state = 'pending';

-- name: FinishUserImport :one
WITH counts AS (
    SELECT
        count(*) FILTER (WHERE state = 'completed')::integer AS completed_count,
        count(*) FILTER (WHERE state = 'failed')::integer AS failed_count
    FROM user_import_entries
    WHERE import_id = $1
)
UPDATE user_imports
SET
    state = CASE WHEN counts.failed_count > 0 THEN 'failed'::user_import_state ELSE 'completed'::user_import_state END,
    completed_count = counts.completed_count,
    failed_count = counts.failed_count,
    finished_at = now()
FROM counts
WHERE id = $1
RETURNING user_imports.*;

-- name: DeleteFinishedUserImportsBefore :execrows
DELETE FROM user_imports
WHERE state IN ('completed', 'failed') AND finished_at < $1;
