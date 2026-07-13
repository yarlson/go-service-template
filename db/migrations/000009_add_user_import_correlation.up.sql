ALTER TABLE user_imports
    ADD COLUMN correlation_id text;

UPDATE user_imports SET correlation_id = id::text;

ALTER TABLE user_imports
    ALTER COLUMN correlation_id SET NOT NULL,
    ADD CONSTRAINT user_imports_correlation_id_not_empty CHECK (correlation_id <> '');
