package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type UserNotificationOutbox struct {
	ID        string
	UserID    string
	Type      string
	Title     *string
	Content   string
	ActionURL *string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (n *UserNotificationOutbox) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	n.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO user_notifications_outbox (id, user_id, type, title, content, action_url, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		n.ID, n.UserID, n.Type, n.Title, n.Content, n.ActionURL, "PENDING",
	).Scan(&n.CreatedAt, &n.UpdatedAt)
}

func (n *UserNotificationOutbox) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM user_notifications_outbox WHERE id = $1", n.ID)
	return err
}

func (n *UserNotificationOutbox) GetManyWithLock(db *sql.DB, ctx context.Context, page, limit int) ([]UserNotificationOutbox, int64, error) {
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
		SELECT id, user_id, type, title, content, action_url, status, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM user_notifications_outbox
		WHERE status = 'PENDING'
		ORDER BY created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED;
		`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]UserNotificationOutbox, 0)
	var total int64
	for rows.Next() {
		var item UserNotificationOutbox
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Type, &item.Title, &item.Content, &item.ActionURL, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
