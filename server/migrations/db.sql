-- ============================================================
-- Função global para atualização automática de updated_at
-- ============================================================

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- ============================================================
-- USERS
-- ============================================================

CREATE TABLE users (
    id UUID PRIMARY KEY,

    name VARCHAR(255) NOT NULL,
    username VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    password_hash TEXT NOT NULL,

    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT users_username_unique UNIQUE (username),
    CONSTRAINT users_email_unique UNIQUE (email),

    CONSTRAINT users_status_check
        CHECK (status IN (
            'ACTIVE',
            'BLOCKED',
            'DISABLED'
        ))
);


-- ============================================================
-- COMPANIES
-- ============================================================

CREATE TABLE companies (
    id UUID PRIMARY KEY,

    name VARCHAR(255) NOT NULL,

    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT companies_status_check
        CHECK (status IN (
            'ACTIVE',
            'SUSPENDED',
            'DISABLED'
        ))
);


-- ============================================================
-- COMPANY USERS
--
-- Relação N:N:
--
-- users <-> companies
-- ============================================================

CREATE TABLE company_users (
    id UUID PRIMARY KEY,

    company_id UUID NOT NULL,
    user_id UUID NOT NULL,

    role VARCHAR(30) NOT NULL DEFAULT 'MEMBER',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT company_users_company_fk
        FOREIGN KEY (company_id)
        REFERENCES companies(id)
        ON DELETE CASCADE,

    CONSTRAINT company_users_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT company_users_unique
        UNIQUE (company_id, user_id),

    CONSTRAINT company_users_role_check
        CHECK (role IN (
            'OWNER',
            'ADMIN',
            'MEMBER',
            'GUEST'
        ))
);


-- ============================================================
-- WORKSPACES
--
-- Cada workspace pertence a uma company.
-- ============================================================

CREATE TABLE workspaces (
    id UUID PRIMARY KEY,

    company_id UUID NOT NULL,

    name VARCHAR(255) NOT NULL,
    slug VARCHAR(150) NOT NULL,

    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT workspaces_company_fk
        FOREIGN KEY (company_id)
        REFERENCES companies(id)
        ON DELETE CASCADE,

    CONSTRAINT workspaces_company_slug_unique
        UNIQUE (company_id, slug),

    CONSTRAINT workspaces_status_check
        CHECK (status IN (
            'ACTIVE',
            'SUSPENDED',
            'DISABLED'
        ))
);


-- ============================================================
-- WORKSPACE USERS
--
-- Relação N:N:
--
-- users <-> workspaces
-- ============================================================

CREATE TABLE workspace_users (
    id UUID PRIMARY KEY,

    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,

    role VARCHAR(30) NOT NULL DEFAULT 'MEMBER',

    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT workspace_users_workspace_fk
        FOREIGN KEY (workspace_id)
        REFERENCES workspaces(id)
        ON DELETE CASCADE,

    CONSTRAINT workspace_users_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT workspace_users_unique
        UNIQUE (workspace_id, user_id),

    CONSTRAINT workspace_users_role_check
        CHECK (role IN (
            'OWNER',
            'ADMIN',
            'MEMBER',
            'GUEST'
        )),

    CONSTRAINT workspace_users_status_check
        CHECK (status IN (
            'ACTIVE',
            'BLOCKED',
            'REMOVED'
        ))
);


-- ============================================================
-- CHANNELS
--
-- Cada channel pertence a um workspace.
-- ============================================================

CREATE TABLE channels (
    id UUID PRIMARY KEY,

    workspace_id UUID NOT NULL,

    name VARCHAR(255) NOT NULL,
    description TEXT,

    type VARCHAR(30) NOT NULL DEFAULT 'PUBLIC',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT channels_workspace_fk
        FOREIGN KEY (workspace_id)
        REFERENCES workspaces(id)
        ON DELETE CASCADE,

    CONSTRAINT channels_workspace_name_unique
        UNIQUE (workspace_id, name),

    CONSTRAINT channels_type_check
        CHECK (type IN (
            'PUBLIC',
            'PRIVATE'
        ))
);


-- ============================================================
-- CHANNEL USERS
--
-- Relação N:N:
--
-- users <-> channels
-- ============================================================

CREATE TABLE channel_users (
    id UUID PRIMARY KEY,

    channel_id UUID NOT NULL,
    user_id UUID NOT NULL,

    role VARCHAR(30) NOT NULL DEFAULT 'MEMBER',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT channel_users_channel_fk
        FOREIGN KEY (channel_id)
        REFERENCES channels(id)
        ON DELETE CASCADE,

    CONSTRAINT channel_users_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT channel_users_unique
        UNIQUE (channel_id, user_id),

    CONSTRAINT channel_users_role_check
        CHECK (role IN (
            'OWNER',
            'ADMIN',
            'MEMBER'
        ))
);


-- ============================================================
-- CHATS
--
-- Todo chat pertence a um workspace.
--
-- Um chat pode opcionalmente estar ligado a um channel.
--
-- DIRECT  -> channel_id NULL
-- GROUP   -> channel_id NULL
-- CHANNEL -> channel_id obrigatório
-- ============================================================

CREATE TABLE chats (
    id UUID PRIMARY KEY,

    workspace_id UUID NOT NULL,
    channel_id UUID NULL,

    type VARCHAR(30) NOT NULL,

    name VARCHAR(255),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chats_workspace_fk
        FOREIGN KEY (workspace_id)
        REFERENCES workspaces(id)
        ON DELETE CASCADE,

    CONSTRAINT chats_channel_fk
        FOREIGN KEY (channel_id)
        REFERENCES channels(id)
        ON DELETE CASCADE,

    CONSTRAINT chats_type_check
        CHECK (type IN (
            'DIRECT',
            'GROUP',
            'CHANNEL'
        )),

    CONSTRAINT chats_channel_type_check
        CHECK (
            (
                type = 'CHANNEL'
                AND channel_id IS NOT NULL
            )
            OR
            (
                type IN ('DIRECT', 'GROUP')
                AND channel_id IS NULL
            )
        )
);


-- Um channel possui somente um chat principal.
CREATE UNIQUE INDEX chats_channel_unique
ON chats(channel_id)
WHERE channel_id IS NOT NULL;


-- ============================================================
-- CHAT USERS
--
-- Relação N:N:
--
-- users <-> chats
--
-- Também mantém posição de leitura.
-- ============================================================

CREATE TABLE chat_users (
    id UUID PRIMARY KEY,

    chat_id UUID NOT NULL,
    user_id UUID NOT NULL,

    last_read_message_id UUID NULL,

    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chat_users_chat_fk
        FOREIGN KEY (chat_id)
        REFERENCES chats(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_users_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_users_unique
        UNIQUE (chat_id, user_id)
);


-- ============================================================
-- CHAT MESSAGES
-- ============================================================

CREATE TABLE chat_messages (
    id UUID PRIMARY KEY,

    chat_id UUID NOT NULL,
    user_id UUID NOT NULL,

    -- UUID gerado no cliente/aplicação para permitir retry
    -- idempotente do envio da mensagem.
    client_message_id UUID NOT NULL,

    -- Ordem lógica da mensagem dentro do chat.
    sequence BIGINT NOT NULL,

    type VARCHAR(30) NOT NULL DEFAULT 'TEXT',

    content TEXT NOT NULL,

    edited_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chat_messages_chat_fk
        FOREIGN KEY (chat_id)
        REFERENCES chats(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_messages_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE RESTRICT,

    CONSTRAINT chat_messages_type_check
        CHECK (type IN (
            'TEXT',
            'SYSTEM'
        )),

    CONSTRAINT chat_messages_client_message_unique
        UNIQUE (client_message_id),

    CONSTRAINT chat_messages_chat_sequence_unique
        UNIQUE (chat_id, sequence)
);


-- ============================================================
-- Agora que chat_messages existe, podemos criar a FK da
-- posição de leitura de chat_users.
-- ============================================================

ALTER TABLE chat_users
ADD CONSTRAINT chat_users_last_read_message_fk
FOREIGN KEY (last_read_message_id)
REFERENCES chat_messages(id)
ON DELETE SET NULL;


-- ============================================================
-- CHAT MESSAGE REACTIONS
-- ============================================================

CREATE TABLE chat_messages_reactions (
    id UUID PRIMARY KEY,

    chat_message_id UUID NOT NULL,
    user_id UUID NOT NULL,

    reaction VARCHAR(100) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chat_messages_reactions_message_fk
        FOREIGN KEY (chat_message_id)
        REFERENCES chat_messages(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_messages_reactions_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_messages_reactions_unique
        UNIQUE (
            chat_message_id,
            user_id,
            reaction
        )
);


-- ============================================================
-- USER NOTIFICATIONS
-- ============================================================

CREATE TABLE user_notifications (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL,

    type VARCHAR(50) NOT NULL,

    title VARCHAR(255),
    content TEXT NOT NULL,

    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    read_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT user_notifications_user_fk
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);


-- ============================================================
-- ÍNDICES DE FOREIGN KEYS / CONSULTAS FREQUENTES
-- ============================================================

CREATE INDEX company_users_user_idx
    ON company_users(user_id);

CREATE INDEX company_users_company_idx
    ON company_users(company_id);


CREATE INDEX workspaces_company_idx
    ON workspaces(company_id);


CREATE INDEX workspace_users_user_idx
    ON workspace_users(user_id);

CREATE INDEX workspace_users_workspace_idx
    ON workspace_users(workspace_id);


CREATE INDEX channels_workspace_idx
    ON channels(workspace_id);


CREATE INDEX channel_users_user_idx
    ON channel_users(user_id);

CREATE INDEX channel_users_channel_idx
    ON channel_users(channel_id);


CREATE INDEX chats_workspace_idx
    ON chats(workspace_id);


CREATE INDEX chat_users_user_idx
    ON chat_users(user_id);

CREATE INDEX chat_users_chat_idx
    ON chat_users(chat_id);


CREATE INDEX chat_messages_chat_idx
    ON chat_messages(chat_id);

CREATE INDEX chat_messages_user_idx
    ON chat_messages(user_id);

CREATE INDEX chat_messages_chat_created_idx
    ON chat_messages(chat_id, created_at DESC);

CREATE INDEX chat_messages_chat_sequence_idx
    ON chat_messages(chat_id, sequence DESC);


CREATE INDEX chat_messages_reactions_message_idx
    ON chat_messages_reactions(chat_message_id);

CREATE INDEX chat_messages_reactions_user_idx
    ON chat_messages_reactions(user_id);


CREATE INDEX user_notifications_user_idx
    ON user_notifications(user_id);

CREATE INDEX user_notifications_user_unread_idx
    ON user_notifications(user_id, created_at DESC)
    WHERE is_read = FALSE;


-- ============================================================
-- TRIGGERS UPDATED_AT
-- ============================================================

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER companies_set_updated_at
BEFORE UPDATE ON companies
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER company_users_set_updated_at
BEFORE UPDATE ON company_users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER workspaces_set_updated_at
BEFORE UPDATE ON workspaces
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER workspace_users_set_updated_at
BEFORE UPDATE ON workspace_users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER channels_set_updated_at
BEFORE UPDATE ON channels
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER channel_users_set_updated_at
BEFORE UPDATE ON channel_users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER chats_set_updated_at
BEFORE UPDATE ON chats
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER chat_users_set_updated_at
BEFORE UPDATE ON chat_users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER chat_messages_set_updated_at
BEFORE UPDATE ON chat_messages
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER chat_messages_reactions_set_updated_at
BEFORE UPDATE ON chat_messages_reactions
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();


CREATE TRIGGER user_notifications_set_updated_at
BEFORE UPDATE ON user_notifications
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
