package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type ChatUser struct {
	ID                string
	ChatID            string
	UserID            string
	LastReadMessageID *string
	JoinedAt          time.Time
	LeftAt            *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (cu *ChatUser) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	cu.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO chat_users (id, chat_id, user_id, last_read_message_id, left_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING joined_at, created_at, updated_at`,
		cu.ID, cu.ChatID, cu.UserID, cu.LastReadMessageID, cu.LeftAt,
	).Scan(&cu.JoinedAt, &cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *ChatUser) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE chat_users
		SET last_read_message_id = $1, left_at = $2
		WHERE id = $3
		RETURNING updated_at`, cu.LastReadMessageID, cu.LeftAt, cu.ID,
	).Scan(&cu.UpdatedAt)
}

func (cu *ChatUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chat_users WHERE id = $1", cu.ID)
	return err
}

func (cu *ChatUser) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_id, user_id, last_read_message_id, joined_at, left_at, created_at, updated_at
		FROM chat_users WHERE id = $1`, cu.ID,
	).Scan(&cu.ID, &cu.ChatID, &cu.UserID, &cu.LastReadMessageID, &cu.JoinedAt, &cu.LeftAt, &cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *ChatUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatUser, int64, error) {
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
		SELECT id, chat_id, user_id, last_read_message_id, joined_at, left_at, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM chat_users
		WHERE chat_id = $1
		ORDER BY joined_at DESC, id DESC
		LIMIT $2 OFFSET $3`, cu.ChatID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatUser, 0)
	var total int64
	for rows.Next() {
		var item ChatUser
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.UserID, &item.LastReadMessageID, &item.JoinedAt,
			&item.LeftAt, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
