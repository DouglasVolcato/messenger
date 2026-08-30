package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type UserNotification struct {
	ID        string
	UserID    string
	Type      string
	Title     *string
	Content   string
	IsRead    bool
	ReadAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (n *UserNotification) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	n.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO user_notifications (id, user_id, type, title, content, is_read, read_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		n.ID, n.UserID, n.Type, n.Title, n.Content, n.IsRead, n.ReadAt,
	).Scan(&n.CreatedAt, &n.UpdatedAt)
}

func (n *UserNotification) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE user_notifications
		SET type = $1, title = $2, content = $3, is_read = $4, read_at = $5
		WHERE id = $6
		RETURNING updated_at`, n.Type, n.Title, n.Content, n.IsRead, n.ReadAt, n.ID,
	).Scan(&n.UpdatedAt)
}

func (n *UserNotification) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM user_notifications WHERE id = $1", n.ID)
	return err
}

func (n *UserNotification) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, user_id, type, title, content, is_read, read_at, created_at, updated_at
		FROM user_notifications WHERE id = $1`, n.ID,
	).Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Content, &n.IsRead, &n.ReadAt, &n.CreatedAt, &n.UpdatedAt)
}
