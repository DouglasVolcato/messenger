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

	var chatID, chatType, workspaceID string
	if err := tx.QueryRowContext(ctx, "SELECT id, type, workspace_id FROM chats WHERE id = $1 FOR UPDATE", m.ChatID).Scan(&chatID, &chatType, &workspaceID); err != nil {
		return err
	}

	if err := tx.QueryRowContext(ctx, `
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
	).Scan(&m.Sequence, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return err
	}

	if (chatType == "DIRECT" || chatType == "GROUP") && m.Type == "TEXT" {
		var senderName string
		_ = tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = $1`, m.UserID).Scan(&senderName)
		if senderName == "" {
			senderName = "Someone"
		}

		rows, err := tx.QueryContext(ctx, `
			SELECT cu.user_id
			FROM chat_users cu
			JOIN workspace_users wu ON wu.workspace_id = $3 AND wu.user_id = cu.user_id AND wu.status = 'ACTIVE'
			JOIN users u ON u.id = cu.user_id AND u.status = 'ACTIVE'
			WHERE cu.chat_id = $1 AND cu.user_id <> $2 AND cu.left_at IS NULL`, m.ChatID, m.UserID, workspaceID)
		if err != nil {
			return err
		}
		recipients := make([]string, 0)
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return err
			}
			recipients = append(recipients, userID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		title := "New message"
		actionURL := "/workspaces/" + workspaceID + "?chat=" + m.ChatID + "#message-" + m.ID
		for _, userID := range recipients {
			n := UserNotification{
				UserID:    userID,
				Type:      "CHAT_MESSAGE",
				Title:     &title,
				Content:   senderName + " sent you a message.",
				ActionURL: &actionURL,
			}
			if err := n.Create(tx, ctx); err != nil {
				return err
			}
		}
	}

	return nil
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

func (m *ChatMessage) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatMessage, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_id, user_id, client_message_id, sequence, type, content,
		       edited_at, deleted_at, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chat_messages
		WHERE chat_id = $1
		ORDER BY sequence DESC
		LIMIT $2 OFFSET $3`, m.ChatID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatMessage, 0)
	var total int64
	for rows.Next() {
		var item ChatMessage
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.UserID, &item.ClientMessageID, &item.Sequence,
			&item.Type, &item.Content, &item.EditedAt, &item.DeletedAt, &item.CreatedAt,
			&item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
