CREATE TABLE messages (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel_id BIGINT NOT NULL
        REFERENCES channels(id) ON DELETE CASCADE,

    author_id  BIGINT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,

    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);