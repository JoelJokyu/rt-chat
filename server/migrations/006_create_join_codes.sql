CREATE TABLE join_codes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    server_id  BIGINT NOT NULL
        REFERENCES servers(id) ON DELETE CASCADE,

    created_by BIGINT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,

    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);