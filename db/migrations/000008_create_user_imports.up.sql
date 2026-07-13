CREATE TYPE user_import_state AS ENUM ('pending', 'running', 'completed', 'failed');
CREATE TYPE user_import_entry_state AS ENUM ('pending', 'completed', 'failed');

CREATE TABLE user_imports (
    id uuid PRIMARY KEY,
    state user_import_state NOT NULL DEFAULT 'pending',
    total_count integer NOT NULL CHECK (total_count BETWEEN 1 AND 100),
    completed_count integer NOT NULL DEFAULT 0 CHECK (completed_count >= 0),
    failed_count integer NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    CHECK (completed_count + failed_count <= total_count)
);

CREATE TABLE user_import_entries (
    import_id uuid NOT NULL REFERENCES user_imports(id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    email text NOT NULL,
    state user_import_entry_state NOT NULL DEFAULT 'pending',
    PRIMARY KEY (import_id, user_id)
);

CREATE UNIQUE INDEX user_import_entries_email
    ON user_import_entries (import_id, lower(email));

CREATE INDEX user_imports_cleanup
    ON user_imports (finished_at)
    WHERE state IN ('completed', 'failed');
