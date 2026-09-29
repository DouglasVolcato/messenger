CREATE TABLE chat_message_outbox (
    id UUID PRIMARY KEY,
    message_id UUID NOT NULL,
    chat_id UUID NOT NULL,
    company_id UUID NOT NULL,
    sender_user_id UUID NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT 'CHAT_MESSAGE',
    title VARCHAR(255),
    content TEXT NOT NULL,
    action_url TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chat_message_outbox_message_unique
        UNIQUE (message_id),

    CONSTRAINT chat_message_outbox_message_fk
        FOREIGN KEY (message_id)
        REFERENCES messages(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_message_outbox_chat_fk
        FOREIGN KEY (chat_id)
        REFERENCES chats(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_message_outbox_company_fk
        FOREIGN KEY (company_id)
        REFERENCES companies(id)
        ON DELETE CASCADE,

    CONSTRAINT chat_message_outbox_sender_fk
        FOREIGN KEY (sender_user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE INDEX chat_message_outbox_pending_idx
    ON chat_message_outbox(status, created_at, id)
    WHERE status = 'PENDING';

CREATE TRIGGER chat_message_outbox_set_updated_at
BEFORE UPDATE ON chat_message_outbox
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
