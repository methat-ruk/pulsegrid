-- +goose Up
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    slug TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT organizations_slug_format
        CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$' AND char_length(slug) BETWEEN 1 AND 63),
    CONSTRAINT organizations_display_name_nonblank
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 200),
    CONSTRAINT organizations_slug_unique UNIQUE (slug)
);

CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    device_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT devices_key_format
        CHECK (char_length(device_key) BETWEEN 1 AND 128 AND device_key = btrim(device_key)),
    CONSTRAINT devices_display_name_nonblank
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 200),
    CONSTRAINT devices_organization_key_unique UNIQUE (organization_id, device_key)
);

CREATE INDEX devices_organization_created_id_idx
    ON devices (organization_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE devices;
DROP TABLE organizations;
