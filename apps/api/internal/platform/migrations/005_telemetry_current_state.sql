-- +goose Up
CREATE TABLE telemetry_observation_keys (
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    message_id UUID NOT NULL,
    first_ingestion_id UUID NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    temperature_celsius DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (device_id, message_id),
    CONSTRAINT telemetry_observation_keys_first_ingestion_unique UNIQUE (first_ingestion_id),
    CONSTRAINT telemetry_observation_keys_message_id_non_nil
        CHECK (message_id <> '00000000-0000-0000-0000-000000000000'::UUID),
    CONSTRAINT telemetry_observation_keys_first_ingestion_non_nil
        CHECK (first_ingestion_id <> '00000000-0000-0000-0000-000000000000'::UUID),
    CONSTRAINT telemetry_observation_keys_temperature_finite
        CHECK (
            temperature_celsius <> 'NaN'::DOUBLE PRECISION
            AND temperature_celsius <> 'Infinity'::DOUBLE PRECISION
            AND temperature_celsius <> '-Infinity'::DOUBLE PRECISION
        )
);

CREATE TABLE telemetry_observations (
    storage_sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ingestion_id UUID NOT NULL,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    message_id UUID NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    temperature_celsius DOUBLE PRECISION NOT NULL,
    mqtt_duplicate BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT telemetry_observations_ingestion_id_unique UNIQUE (ingestion_id),
    CONSTRAINT telemetry_observations_device_message_unique UNIQUE (device_id, message_id),
    CONSTRAINT telemetry_observations_key_fk
        FOREIGN KEY (device_id, message_id)
        REFERENCES telemetry_observation_keys(device_id, message_id)
        ON DELETE RESTRICT,
    CONSTRAINT telemetry_observations_ingestion_id_non_nil
        CHECK (ingestion_id <> '00000000-0000-0000-0000-000000000000'::UUID),
    CONSTRAINT telemetry_observations_message_id_non_nil
        CHECK (message_id <> '00000000-0000-0000-0000-000000000000'::UUID),
    CONSTRAINT telemetry_observations_temperature_finite
        CHECK (
            temperature_celsius <> 'NaN'::DOUBLE PRECISION
            AND temperature_celsius <> 'Infinity'::DOUBLE PRECISION
            AND temperature_celsius <> '-Infinity'::DOUBLE PRECISION
        )
);

CREATE INDEX telemetry_observations_device_observed_message_idx
    ON telemetry_observations (device_id, observed_at DESC, message_id DESC);

CREATE INDEX telemetry_observations_device_sequence_idx
    ON telemetry_observations (device_id, storage_sequence DESC);

CREATE TABLE device_current_state (
    device_id UUID PRIMARY KEY REFERENCES devices(id) ON DELETE RESTRICT,
    observation_sequence BIGINT NOT NULL REFERENCES telemetry_observations(storage_sequence) ON DELETE RESTRICT,
    message_id UUID NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    temperature_celsius DOUBLE PRECISION NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT device_current_state_message_id_non_nil
        CHECK (message_id <> '00000000-0000-0000-0000-000000000000'::UUID),
    CONSTRAINT device_current_state_temperature_finite
        CHECK (
            temperature_celsius <> 'NaN'::DOUBLE PRECISION
            AND temperature_celsius <> 'Infinity'::DOUBLE PRECISION
            AND temperature_celsius <> '-Infinity'::DOUBLE PRECISION
        )
);

-- +goose Down
DROP TABLE device_current_state;
DROP TABLE telemetry_observations;
DROP TABLE telemetry_observation_keys;
