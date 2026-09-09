package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type ChatUser struct {
	ID        string
	ChatID    string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (cu *ChatUser) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	cu.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO chat_users (id, chat_id, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (chat_id, user_id)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		cu.ID, cu.ChatID, cu.UserID,
	).Scan(&cu.ID, &cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *ChatUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx,
		"DELETE FROM chat_users WHERE chat_id = $1 AND user_id = $2",
		cu.ChatID, cu.UserID,
	)
	return err
}

func (cu *ChatUser) GetOneByChatAndUser(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, user_id, created_at, updated_at
		FROM chat_users
		WHERE chat_id = $1 AND user_id = $2`,
		cu.ChatID, cu.UserID,
	).Scan(&cu.ID, &cu.ChatID, &cu.UserID, &cu.CreatedAt, &cu.UpdatedAt)
}
