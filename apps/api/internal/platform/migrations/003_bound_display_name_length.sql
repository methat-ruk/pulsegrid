-- +goose Up
ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

-- +goose Down
ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (
            char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (
            char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );
