CREATE INDEX messages_active_channel_history_idx
ON messages (channel_id, created_at DESC, id DESC)
WHERE deleted_at IS NULL;
