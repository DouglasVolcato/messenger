package models

import (
	"context"
	"database/sql"
	"errors"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Message struct {
	ID              string
	ChatID          *string
	SenderUserID    string
	RecipientUserID *string
	Direct          bool
	ClientMessageID string
	Sequence        int64
	Content         string
	EditedAt        *time.Time
	DeletedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (m *Message) Create(tx *sql.Tx, ctx context.Context) error {
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

	if m.Direct {
		if m.ChatID != nil || m.RecipientUserID == nil || *m.RecipientUserID == "" || *m.RecipientUserID == m.SenderUserID {
			return errors.New("invalid direct message target")
		}
		m.Sequence = 0
	} else {
		if m.ChatID == nil || *m.ChatID == "" || m.RecipientUserID != nil {
			return errors.New("invalid chat message target")
		}
		var lockedChatID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM chats WHERE id = $1 FOR UPDATE`, *m.ChatID).Scan(&lockedChatID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(sequence), 0) + 1
			FROM messages
			WHERE chat_id = $1`, *m.ChatID,
		).Scan(&m.Sequence); err != nil {
			return err
		}
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO messages (
			id, chat_id, sender_user_id, recipient_user_id, direct,
			client_message_id, sequence, content, edited_at, deleted_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING created_at, updated_at`,
		m.ID, m.ChatID, m.SenderUserID, m.RecipientUserID, m.Direct,
		m.ClientMessageID, m.Sequence, m.Content, m.EditedAt, m.DeletedAt,
	).Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		return err
	}

	var senderName string
	_ = tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = $1`, m.SenderUserID).Scan(&senderName)
	if senderName == "" {
		senderName = "Someone"
	}

	if m.Direct {
		title := "New direct message"
		actionURL := "/messages/users/" + m.SenderUserID
		notification := UserNotification{
			UserID:    *m.RecipientUserID,
			Type:      "DIRECT_MESSAGE",
			Title:     &title,
			Content:   senderName + " sent you a message.",
			ActionURL: &actionURL,
		}
		notification.Create(tx, ctx)

		notificationOutbox := UserNotificationOutbox{
			UserID:    *m.RecipientUserID,
			Type:      "DIRECT_MESSAGE",
			Title:     &title,
			Content:   senderName + " sent you a message.",
			ActionURL: &actionURL,
			Status:    "PENDING",
		}
		notificationOutbox.Create(tx, ctx)
	}

	var companyID string
	if err := tx.QueryRowContext(ctx, `SELECT company_id FROM chats WHERE id = $1`, *m.ChatID).Scan(&companyID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT cu.user_id
		FROM chat_users cu
		JOIN company_users company_member
		  ON company_member.user_id = cu.user_id
		 AND company_member.company_id = $2
		JOIN users u ON u.id = cu.user_id AND u.status = 'ACTIVE'
		WHERE cu.chat_id = $1 AND cu.user_id <> $3`,
		*m.ChatID, companyID, m.SenderUserID,
	)
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

	title := "New chat message"
	actionURL := "/companies/" + companyID + "/chats/" + *m.ChatID + "#message-" + m.ID
	for _, userID := range recipients {
		notification := UserNotification{
			UserID:    userID,
			Type:      "CHAT_MESSAGE",
			Title:     &title,
			Content:   senderName + " sent a message in a chat you follow.",
			ActionURL: &actionURL,
		}
		if err := notification.Create(tx, ctx); err != nil {
			return err
		}
		notificationOutbox := UserNotificationOutbox{
			UserID:    userID,
			Type:      "CHAT_MESSAGE",
			Title:     &title,
			Content:   senderName + " sent a message in a chat you follow.",
			ActionURL: &actionURL,
			Status:    "PENDING",
		}
		if err := notificationOutbox.Create(tx, ctx); err != nil {
			return err
		}
	}

	return nil
}

func (m *Message) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE messages
		SET content = $1, edited_at = $2, deleted_at = $3
		WHERE id = $4
		RETURNING updated_at`,
		m.Content, m.EditedAt, m.DeletedAt, m.ID,
	).Scan(&m.UpdatedAt)
}

func (m *Message) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
		       client_message_id, sequence, content, edited_at, deleted_at,
		       created_at, updated_at
		FROM messages WHERE id = $1`, m.ID,
	).Scan(
		&m.ID, &m.ChatID, &m.SenderUserID, &m.RecipientUserID, &m.Direct,
		&m.ClientMessageID, &m.Sequence, &m.Content, &m.EditedAt, &m.DeletedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
}

func (m *Message) GetOneByClientMessageID(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
		       client_message_id, sequence, content, edited_at, deleted_at,
		       created_at, updated_at
		FROM messages WHERE client_message_id = $1`, m.ClientMessageID,
	).Scan(
		&m.ID, &m.ChatID, &m.SenderUserID, &m.RecipientUserID, &m.Direct,
		&m.ClientMessageID, &m.Sequence, &m.Content, &m.EditedAt, &m.DeletedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
}

func (m *Message) GetChatMessages(db *sql.DB, ctx context.Context, limit int) ([]Message, error) {
	if m.ChatID == nil {
		return nil, errors.New("chat id is required")
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
		       client_message_id, sequence, content, edited_at, deleted_at,
		       created_at, updated_at
		FROM (
			SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
			       client_message_id, sequence, content, edited_at, deleted_at,
			       created_at, updated_at
			FROM messages
			WHERE chat_id = $1 AND direct = FALSE
			ORDER BY sequence DESC
			LIMIT $2
		) latest
		ORDER BY sequence ASC`, *m.ChatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Message, 0)
	for rows.Next() {
		var item Message
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.SenderUserID, &item.RecipientUserID, &item.Direct,
			&item.ClientMessageID, &item.Sequence, &item.Content, &item.EditedAt, &item.DeletedAt,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (m *Message) GetDirectMessages(db *sql.DB, ctx context.Context, otherUserID string, limit int) ([]Message, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
		       client_message_id, sequence, content, edited_at, deleted_at,
		       created_at, updated_at
		FROM (
			SELECT id, chat_id, sender_user_id, recipient_user_id, direct,
			       client_message_id, sequence, content, edited_at, deleted_at,
			       created_at, updated_at
			FROM messages
			WHERE direct = TRUE
			  AND (
				(sender_user_id = $1 AND recipient_user_id = $2)
				OR
				(sender_user_id = $2 AND recipient_user_id = $1)
			  )
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		) latest
		ORDER BY created_at ASC, id ASC`,
		m.SenderUserID, otherUserID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Message, 0)
	for rows.Next() {
		var item Message
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.SenderUserID, &item.RecipientUserID, &item.Direct,
			&item.ClientMessageID, &item.Sequence, &item.Content, &item.EditedAt, &item.DeletedAt,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
