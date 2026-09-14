-- +goose Up
-- Keep this explicit Unicode whitespace set aligned with Go's strings.TrimSpace
-- (unicode.White_Space) instead of depending on the database locale.
ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(btrim(display_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'))
                BETWEEN 1 AND 200
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_key_format,
    ADD CONSTRAINT devices_key_format
        CHECK (
            char_length(device_key) BETWEEN 1 AND 128
            AND device_key = btrim(device_key, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(btrim(display_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'))
                BETWEEN 1 AND 200
        );

-- +goose Down
ALTER TABLE devices
    DROP CONSTRAINT devices_display_name_nonblank,
    ADD CONSTRAINT devices_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );

ALTER TABLE devices
    DROP CONSTRAINT devices_key_format,
    ADD CONSTRAINT devices_key_format
        CHECK (
            char_length(device_key) BETWEEN 1 AND 128
            AND device_key = regexp_replace(device_key, '^[[:space:]]+|[[:space:]]+$', '', 'g')
        );

ALTER TABLE organizations
    DROP CONSTRAINT organizations_display_name_nonblank,
    ADD CONSTRAINT organizations_display_name_nonblank
        CHECK (
            char_length(display_name) BETWEEN 1 AND 200
            AND char_length(regexp_replace(display_name, '^[[:space:]]+|[[:space:]]+$', '', 'g'))
                BETWEEN 1 AND 200
        );
