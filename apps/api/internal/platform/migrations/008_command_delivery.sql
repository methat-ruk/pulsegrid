-- +goose Up
ALTER TABLE commands
    ADD COLUMN dispatch_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN next_dispatch_at TIMESTAMPTZ;

UPDATE commands
SET next_dispatch_at = created_at;

ALTER TABLE commands
    ALTER COLUMN next_dispatch_at SET NOT NULL,
    ALTER COLUMN next_dispatch_at SET DEFAULT CURRENT_TIMESTAMP,
    ADD CONSTRAINT commands_dispatch_attempts_bounded
        CHECK (dispatch_attempts BETWEEN 0 AND 4);

CREATE INDEX commands_dispatch_due_idx
    ON commands (next_dispatch_at, created_at, id)
    WHERE status IN ('PENDING', 'DISPATCHED') AND dispatch_attempts < 4;

-- +goose Down
DROP INDEX commands_dispatch_due_idx;
ALTER TABLE commands
    DROP CONSTRAINT commands_dispatch_attempts_bounded,
    DROP COLUMN dispatch_attempts,
    DROP COLUMN next_dispatch_at;
