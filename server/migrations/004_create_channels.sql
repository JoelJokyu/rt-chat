CREATE TABLE channels (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    server_id  BIGINT NOT NULL
        REFERENCES servers(id) ON DELETE CASCADE,

    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);