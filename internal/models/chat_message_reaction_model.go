package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type ChatMessageReaction struct {
	ID            string
	ChatMessageID string
	UserID        string
	Reaction      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (r *ChatMessageReaction) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	r.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO chat_messages_reactions (id, chat_message_id, user_id, reaction)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		r.ID, r.ChatMessageID, r.UserID, r.Reaction,
	).Scan(&r.CreatedAt, &r.UpdatedAt)
}

func (r *ChatMessageReaction) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE chat_messages_reactions SET reaction = $1 WHERE id = $2
		RETURNING updated_at`, r.Reaction, r.ID,
	).Scan(&r.UpdatedAt)
}

func (r *ChatMessageReaction) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chat_messages_reactions WHERE id = $1", r.ID)
	return err
}

func (r *ChatMessageReaction) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, chat_message_id, user_id, reaction, created_at, updated_at
		FROM chat_messages_reactions WHERE id = $1`, r.ID,
	).Scan(&r.ID, &r.ChatMessageID, &r.UserID, &r.Reaction, &r.CreatedAt, &r.UpdatedAt)
}

func (r *ChatMessageReaction) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatMessageReaction, int64, error) {
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
		SELECT id, chat_message_id, user_id, reaction, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chat_messages_reactions
		WHERE chat_message_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, r.ChatMessageID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatMessageReaction, 0)
	var total int64
	for rows.Next() {
		var item ChatMessageReaction
		if err := rows.Scan(&item.ID, &item.ChatMessageID, &item.UserID, &item.Reaction, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
