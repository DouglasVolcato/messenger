ALTER TABLE users
ADD COLUMN IF NOT EXISTS is_system_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS users_system_admin_idx
ON users (is_system_admin)
WHERE is_system_admin = TRUE;
