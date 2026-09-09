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
	ActionURL *string
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
		INSERT INTO user_notifications (id, user_id, type, title, content, action_url, is_read, read_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at`,
		n.ID, n.UserID, n.Type, n.Title, n.Content, n.ActionURL, n.IsRead, n.ReadAt,
	).Scan(&n.CreatedAt, &n.UpdatedAt)
}

func (n *UserNotification) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE user_notifications
		SET type = $1, title = $2, content = $3, action_url = $4, is_read = $5, read_at = $6
		WHERE id = $7
		RETURNING updated_at`, n.Type, n.Title, n.Content, n.ActionURL, n.IsRead, n.ReadAt, n.ID,
	).Scan(&n.UpdatedAt)
}

func (n *UserNotification) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM user_notifications WHERE id = $1", n.ID)
	return err
}

func (n *UserNotification) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, user_id, type, title, content, action_url, is_read, read_at, created_at, updated_at
		FROM user_notifications WHERE id = $1`, n.ID,
	).Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Content, &n.ActionURL, &n.IsRead, &n.ReadAt, &n.CreatedAt, &n.UpdatedAt)
}

func (n *UserNotification) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]UserNotification, int64, error) {
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
		SELECT id, user_id, type, title, content, action_url, is_read, read_at, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM user_notifications
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, n.UserID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]UserNotification, 0)
	var total int64
	for rows.Next() {
		var item UserNotification
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Type, &item.Title, &item.Content, &item.ActionURL, &item.IsRead,
			&item.ReadAt, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
