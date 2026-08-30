package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type ChatMessage struct {
	ID              string
	ChatID          string
	UserID          string
	ClientMessageID string
	Sequence        int64
	Type            string
	Content         string
	EditedAt        *time.Time
	DeletedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (m *ChatMessage) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	m.ID = id

	if m.ClientMessageID == "" {
		m.ClientMessageID, err = utils.GenerateUUID()
		if err != nil {
			return err
		}
	}
	if m.Type == "" {
		m.Type = "TEXT"
	}

	var chatID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM chats WHERE id = $1 FOR UPDATE", m.ChatID).Scan(&chatID); err != nil {
		return err
	}

	return tx.QueryRowContext(ctx, `
		WITH next_sequence AS (
			SELECT COALESCE(MAX(sequence), 0) + 1 AS sequence
			FROM chat_messages
			WHERE chat_id = $2
		)
		INSERT INTO chat_messages (
			id, chat_id, user_id, client_message_id, sequence, type, content, edited_at, deleted_at
		)
		SELECT $1, $2, $3, $4, ns.sequence, $5, $6, $7, $8
		FROM next_sequence ns
		RETURNING sequence, created_at, updated_at`,
		m.ID, m.ChatID, m.UserID, m.ClientMessageID, m.Type, m.Content, m.EditedAt, m.DeletedAt,
	).Scan(&m.Sequence, &m.CreatedAt, &m.UpdatedAt)
}

func (m *ChatMessage) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE chat_messages
		SET type = $1, content = $2, edited_at = $3, deleted_at = $4
		WHERE id = $5
		RETURNING updated_at`, m.Type, m.Content, m.EditedAt, m.DeletedAt, m.ID,
	).Scan(&m.UpdatedAt)
}

func (m *ChatMessage) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chat_messages WHERE id = $1", m.ID)
	return err
}

func (m *ChatMessage) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, user_id, client_message_id, sequence, type, content,
		       edited_at, deleted_at, created_at, updated_at
		FROM chat_messages
		WHERE id = $1`, m.ID,
	).Scan(
		&m.ID, &m.ChatID, &m.UserID, &m.ClientMessageID, &m.Sequence, &m.Type, &m.Content,
		&m.EditedAt, &m.DeletedAt, &m.CreatedAt, &m.UpdatedAt,
	)
}

func (m *ChatMessage) GetOneByClientMessageID(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, user_id, client_message_id, sequence, type, content,
		       edited_at, deleted_at, created_at, updated_at
		FROM chat_messages
		WHERE client_message_id = $1`, m.ClientMessageID,
	).Scan(
		&m.ID, &m.ChatID, &m.UserID, &m.ClientMessageID, &m.Sequence, &m.Type, &m.Content,
		&m.EditedAt, &m.DeletedAt, &m.CreatedAt, &m.UpdatedAt,
	)
}
