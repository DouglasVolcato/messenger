package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type MessageReaction struct {
	ID        string
	MessageID string
	UserID    string
	Reaction  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (r *MessageReaction) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	r.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO messages_reactions (id, message_id, user_id, reaction)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (message_id, user_id, reaction)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		r.ID, r.MessageID, r.UserID, r.Reaction,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func (r *MessageReaction) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM messages_reactions
		WHERE message_id = $1 AND user_id = $2 AND reaction = $3`,
		r.MessageID, r.UserID, r.Reaction,
	)
	return err
}
