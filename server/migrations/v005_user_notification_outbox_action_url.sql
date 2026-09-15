ALTER TABLE user_notifications_outbox
ADD COLUMN IF NOT EXISTS action_url TEXT NULL;
