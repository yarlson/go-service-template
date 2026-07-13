CREATE TABLE user_permissions (
    user_id uuid PRIMARY KEY,
    revision bigint NOT NULL CHECK (revision > 0),
    permissions text[] NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE processed_events (
    event_id text PRIMARY KEY CHECK (event_id <> ''),
    event_type text NOT NULL CHECK (event_type <> ''),
    processed_at timestamptz NOT NULL DEFAULT now()
);
