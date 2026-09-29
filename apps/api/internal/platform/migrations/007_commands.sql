-- +goose Up
ALTER TABLE devices
    ADD CONSTRAINT devices_organization_id_id_unique UNIQUE (organization_id, id);

CREATE TABLE commands (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    device_id UUID NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    idempotency_key UUID NOT NULL,
    created_request_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    dispatched_at TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    terminal_at TIMESTAMPTZ,
    failure_code TEXT,
    CONSTRAINT commands_device_organization_fk
        FOREIGN KEY (organization_id, device_id)
        REFERENCES devices (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT commands_type_supported CHECK (type = 'PING'),
    CONSTRAINT commands_status_supported CHECK (
        status IN ('PENDING', 'DISPATCHED', 'ACKNOWLEDGED', 'COMPLETED', 'FAILED', 'TIMED_OUT')
    ),
    CONSTRAINT commands_idempotency_unique UNIQUE (organization_id, idempotency_key),
    CONSTRAINT commands_request_id_bounded CHECK (char_length(created_request_id) <= 64),
    CONSTRAINT commands_failure_state_consistent CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL
            AND failure_code IN ('DEVICE_REPORTED_FAILURE', 'DELIVERY_FAILED'))
        OR (status <> 'FAILED' AND failure_code IS NULL)
    ),
    CONSTRAINT commands_terminal_time_consistent CHECK (
        (status IN ('COMPLETED', 'FAILED', 'TIMED_OUT') AND terminal_at IS NOT NULL)
        OR (status NOT IN ('COMPLETED', 'FAILED', 'TIMED_OUT') AND terminal_at IS NULL)
    ),
    CONSTRAINT commands_ack_requires_dispatch CHECK (
        acknowledged_at IS NULL OR dispatched_at IS NOT NULL
    ),
    CONSTRAINT commands_pending_has_no_dispatch CHECK (
        status <> 'PENDING' OR dispatched_at IS NULL
    ),
    CONSTRAINT commands_dispatched_time_consistent CHECK (
        status <> 'DISPATCHED' OR (dispatched_at IS NOT NULL AND acknowledged_at IS NULL)
    ),
    CONSTRAINT commands_ack_time_consistent CHECK (
        status <> 'ACKNOWLEDGED' OR acknowledged_at IS NOT NULL
    ),
    CONSTRAINT commands_completed_has_receipt CHECK (
        status <> 'COMPLETED' OR acknowledged_at IS NOT NULL
    ),
    CONSTRAINT commands_device_failure_has_receipt CHECK (
        failure_code <> 'DEVICE_REPORTED_FAILURE' OR acknowledged_at IS NOT NULL
    ),
    CONSTRAINT commands_expiry_after_create CHECK (expires_at > created_at),
    CONSTRAINT commands_idempotency_key_non_nil
        CHECK (idempotency_key <> '00000000-0000-0000-0000-000000000000'::UUID)
);

CREATE INDEX commands_device_created_id_idx
    ON commands (organization_id, device_id, created_at DESC, id DESC);

CREATE INDEX commands_expiry_id_idx
    ON commands (expires_at, id)
    WHERE status IN ('PENDING', 'DISPATCHED', 'ACKNOWLEDGED');

-- +goose Down
DROP TABLE commands;
ALTER TABLE devices
    DROP CONSTRAINT devices_organization_id_id_unique;
