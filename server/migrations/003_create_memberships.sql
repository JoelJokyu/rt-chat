CREATE TABLE memberships (
    server_id BIGINT NOT NULL
        REFERENCES servers(id) ON DELETE CASCADE,

    user_id BIGINT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,

    role TEXT NOT NULL
        CHECK (role IN ('member', 'admin', 'owner')),

    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (server_id, user_id)
);

CREATE UNIQUE INDEX one_owner_per_server
    ON memberships (server_id)
    WHERE role = 'owner';