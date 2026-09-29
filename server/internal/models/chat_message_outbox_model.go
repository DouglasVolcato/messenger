package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
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

func (o *ChatMessageOutbox) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	o.ID = id
	if o.Type == "" {
		o.Type = "CHAT_MESSAGE"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO chat_message_outbox (
			id, message_id, chat_id, company_id, sender_user_id,
			type, title, content, action_url, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'PENDING')
		RETURNING created_at, updated_at`,
		o.ID, o.MessageID, o.ChatID, o.CompanyID, o.SenderUserID,
		o.Type, o.Title, o.Content, o.ActionURL,
	).Scan(&o.CreatedAt, &o.UpdatedAt)
}
