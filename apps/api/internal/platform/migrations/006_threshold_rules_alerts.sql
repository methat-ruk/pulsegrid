-- +goose Up
CREATE TABLE threshold_rules (
    id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    metric TEXT NOT NULL DEFAULT 'TEMPERATURE_CELSIUS',
    comparator TEXT NOT NULL,
    threshold_celsius DOUBLE PRECISION NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    revision INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT threshold_rules_device_id_unique UNIQUE (id, device_id),
    CONSTRAINT threshold_rules_metric_supported CHECK (metric = 'TEMPERATURE_CELSIUS'),
    CONSTRAINT threshold_rules_comparator_supported CHECK (comparator IN ('GT', 'GTE', 'LT', 'LTE')),
    CONSTRAINT threshold_rules_threshold_finite CHECK (
        threshold_celsius <> 'NaN'::DOUBLE PRECISION
        AND threshold_celsius <> 'Infinity'::DOUBLE PRECISION
        AND threshold_celsius <> '-Infinity'::DOUBLE PRECISION
    ),
    CONSTRAINT threshold_rules_revision_positive CHECK (revision > 0)
);

CREATE INDEX threshold_rules_device_id_idx
    ON threshold_rules (device_id, id);

CREATE TABLE threshold_alerts (
    id UUID PRIMARY KEY,
    rule_id UUID NOT NULL,
    device_id UUID NOT NULL,
    message_id UUID NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    temperature_celsius DOUBLE PRECISION NOT NULL,
    metric TEXT NOT NULL,
    comparator TEXT NOT NULL,
    threshold_celsius DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT threshold_alerts_rule_device_fk
        FOREIGN KEY (rule_id, device_id)
        REFERENCES threshold_rules (id, device_id) ON DELETE RESTRICT,
    CONSTRAINT threshold_alerts_observation_key_fk
        FOREIGN KEY (device_id, message_id)
        REFERENCES telemetry_observation_keys (device_id, message_id) ON DELETE RESTRICT,
    CONSTRAINT threshold_alerts_rule_message_unique UNIQUE (rule_id, device_id, message_id),
    CONSTRAINT threshold_alerts_metric_supported CHECK (metric = 'TEMPERATURE_CELSIUS'),
    CONSTRAINT threshold_alerts_comparator_supported CHECK (comparator IN ('GT', 'GTE', 'LT', 'LTE')),
    CONSTRAINT threshold_alerts_temperature_finite CHECK (
        temperature_celsius <> 'NaN'::DOUBLE PRECISION
        AND temperature_celsius <> 'Infinity'::DOUBLE PRECISION
        AND temperature_celsius <> '-Infinity'::DOUBLE PRECISION
    ),
    CONSTRAINT threshold_alerts_threshold_finite CHECK (
        threshold_celsius <> 'NaN'::DOUBLE PRECISION
        AND threshold_celsius <> 'Infinity'::DOUBLE PRECISION
        AND threshold_celsius <> '-Infinity'::DOUBLE PRECISION
    ),
    CONSTRAINT threshold_alerts_message_id_non_nil
        CHECK (message_id <> '00000000-0000-0000-0000-000000000000'::UUID)
);

CREATE INDEX threshold_alerts_created_id_idx
    ON threshold_alerts (created_at DESC, id DESC);

CREATE INDEX threshold_alerts_device_created_id_idx
    ON threshold_alerts (device_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE threshold_alerts;
DROP TABLE threshold_rules;
