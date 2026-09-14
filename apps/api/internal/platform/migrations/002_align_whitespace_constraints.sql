-- +goose Up
ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (
            char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_key_format,
    ADD CONSTRAINT devices_key_format
        CHECK (
            char_length(device_key) BETWEEN 1 AND 128
            AND device_key = regexp_replace(device_key, '^[[:space:]]+|[[:space:]]+$', '', 'g')
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (
            char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

-- +goose Down
ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 200);

ALTER TABLE devices
    DROP CONSTRAINT devices_key_format,
    ADD CONSTRAINT devices_key_format
        CHECK (char_length(device_key) BETWEEN 1 AND 128 AND device_key = btrim(device_key));

ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 200);
