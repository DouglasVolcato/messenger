package models

import (
	"context"
	"database/sql"
	"time"
)

type ChatMessageOutbox struct {
	ID           string
	MessageID    string
	ChatID       string
	CompanyID    string
	SenderUserID string
	Type         string
	Title        *string
	Content      string
	ActionURL    *string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (o *ChatMessageOutbox) GetManyWithLock(tx *sql.Tx, ctx context.Context, limit int) ([]ChatMessageOutbox, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, message_id, chat_id, company_id, sender_user_id,
		       type, title, content, action_url, status, created_at, updated_at
		FROM chat_message_outbox
		WHERE status = 'PENDING'
		ORDER BY created_at ASC, id ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ChatMessageOutbox, 0)
	for rows.Next() {
		var item ChatMessageOutbox
		if err := rows.Scan(
			&item.ID, &item.MessageID, &item.ChatID, &item.CompanyID, &item.SenderUserID,
			&item.Type, &item.Title, &item.Content, &item.ActionURL, &item.Status, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (o *ChatMessageOutbox) CreateDurableNotifications(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO user_notifications (
			id, user_id, type, title, content, action_url, is_read, read_at
		)
		SELECT
			md5($1 || cu.user_id::text)::uuid,
			cu.user_id,
			$2, $3, $4, $5, FALSE, NULL
		FROM chat_users cu
		JOIN users u
		  ON u.id = cu.user_id AND u.status = 'ACTIVE'
		JOIN company_users company_member
		  ON company_member.user_id = cu.user_id
		 AND company_member.company_id = $6
		WHERE cu.chat_id = $7
		  AND cu.user_id <> $8
		ON CONFLICT (id) DO NOTHING`,
		o.ID, o.Type, o.Title, o.Content, o.ActionURL, o.CompanyID, o.ChatID, o.SenderUserID,
	)
	return err
}

func (o *ChatMessageOutbox) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chat_message_outbox WHERE id = $1", o.ID)
	return err
}
