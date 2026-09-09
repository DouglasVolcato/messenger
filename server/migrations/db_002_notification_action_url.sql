ALTER TABLE user_notifications
ADD COLUMN IF NOT EXISTS action_url TEXT NULL;

CREATE INDEX IF NOT EXISTS user_notifications_user_read_idx
ON user_notifications(user_id, is_read, created_at DESC);
