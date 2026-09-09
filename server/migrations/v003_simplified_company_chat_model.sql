-- Simplify the messaging domain to companies, chats, direct messages and subscriptions.
-- This migration preserves users, companies, company memberships, chat memberships,
-- messages, reactions and notifications while removing workspaces/channels.

-- Company memberships now have only two business roles.
ALTER TABLE company_users DROP CONSTRAINT IF EXISTS company_users_role_check;
ALTER TABLE company_users ALTER COLUMN role SET DEFAULT 'USER';
UPDATE company_users
SET role = CASE WHEN role IN ('OWNER', 'ADMIN') THEN 'ADMIN' ELSE 'USER' END;
ALTER TABLE company_users
ADD CONSTRAINT company_users_role_check CHECK (role IN ('ADMIN', 'USER'));

-- Resolve each existing chat to its owning company before removing workspaces/channels.
ALTER TABLE chats ADD COLUMN IF NOT EXISTS company_id UUID;
UPDATE chats ch
SET company_id = w.company_id
FROM workspaces w
WHERE ch.workspace_id = w.id
  AND ch.company_id IS NULL;

-- Preserve old unnamed direct/group chats as regular company chats.
UPDATE chats SET name = 'Chat' WHERE name IS NULL OR BTRIM(name) = '';

DROP INDEX IF EXISTS chats_channel_unique;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_channel_fk;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_workspace_fk;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_channel_type_check;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_type_check;

ALTER TABLE chats DROP COLUMN IF EXISTS workspace_id;
ALTER TABLE chats DROP COLUMN IF EXISTS channel_id;
ALTER TABLE chats DROP COLUMN IF EXISTS type;

ALTER TABLE chats ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE chats ALTER COLUMN name SET NOT NULL;

ALTER TABLE chats
ADD CONSTRAINT chats_company_fk
FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS chats_company_idx ON chats(company_id);

-- Chat subscriptions are intentionally simple: membership exists or it does not.
ALTER TABLE chat_users DROP CONSTRAINT IF EXISTS chat_users_last_read_message_fk;
ALTER TABLE chat_users DROP COLUMN IF EXISTS last_read_message_id;
ALTER TABLE chat_users DROP COLUMN IF EXISTS joined_at;
ALTER TABLE chat_users DROP COLUMN IF EXISTS left_at;

-- Rename the message tables to match the simplified domain.
ALTER TABLE chat_messages RENAME TO messages;
ALTER TABLE messages RENAME COLUMN user_id TO sender_user_id;

ALTER TABLE messages ADD COLUMN IF NOT EXISTS recipient_user_id UUID;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS direct BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE messages DROP CONSTRAINT IF EXISTS chat_messages_type_check;
ALTER TABLE messages DROP COLUMN IF EXISTS type;

ALTER TABLE messages ALTER COLUMN chat_id DROP NOT NULL;

ALTER TABLE messages
ADD CONSTRAINT messages_recipient_user_fk
FOREIGN KEY (recipient_user_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE messages
ADD CONSTRAINT messages_target_check CHECK (
    (direct = TRUE AND recipient_user_id IS NOT NULL AND chat_id IS NULL AND sender_user_id <> recipient_user_id)
    OR
    (direct = FALSE AND recipient_user_id IS NULL AND chat_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS messages_chat_idx ON messages(chat_id);
CREATE INDEX IF NOT EXISTS messages_sender_idx ON messages(sender_user_id);
CREATE INDEX IF NOT EXISTS messages_recipient_idx ON messages(recipient_user_id);
CREATE INDEX IF NOT EXISTS messages_direct_pair_idx
    ON messages(sender_user_id, recipient_user_id, created_at DESC)
    WHERE direct = TRUE;

ALTER TABLE chat_messages_reactions RENAME TO messages_reactions;
ALTER TABLE messages_reactions RENAME COLUMN chat_message_id TO message_id;

CREATE INDEX IF NOT EXISTS messages_reactions_message_idx ON messages_reactions(message_id);
CREATE INDEX IF NOT EXISTS messages_reactions_user_idx ON messages_reactions(user_id);

-- The simplified application has no workspace/channel layer.
DROP TABLE IF EXISTS channel_users CASCADE;
DROP TABLE IF EXISTS channels CASCADE;
DROP TABLE IF EXISTS workspace_users CASCADE;
DROP TABLE IF EXISTS workspaces CASCADE;

-- Company administration is represented by company_users.role.
ALTER TABLE users DROP COLUMN IF EXISTS is_system_admin;

-- Old links pointed to removed workspace pages.
UPDATE user_notifications
SET action_url = NULL
WHERE action_url LIKE '/workspaces/%';
