package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Chat struct {
	ID          string
	WorkspaceID string
	ChannelID   *string
	Type        string
	Name        *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (c *Chat) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	c.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO chats (id, workspace_id, channel_id, type, name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		c.ID, c.WorkspaceID, c.ChannelID, c.Type, c.Name,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (c *Chat) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE chats SET channel_id = $1, type = $2, name = $3 WHERE id = $4
		RETURNING updated_at`, c.ChannelID, c.Type, c.Name, c.ID,
	).Scan(&c.UpdatedAt)
}

func (c *Chat) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chats WHERE id = $1", c.ID)
	return err
}

func (c *Chat) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, workspace_id, channel_id, type, name, created_at, updated_at
		FROM chats WHERE id = $1`, c.ID,
	).Scan(&c.ID, &c.WorkspaceID, &c.ChannelID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt)
}

func (c *Chat) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Chat, int64, error) {
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
		SELECT id, workspace_id, channel_id, type, name, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chats
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, c.WorkspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Chat, 0)
	var total int64
	for rows.Next() {
		var item Chat
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.ChannelID, &item.Type, &item.Name, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
