package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Chat struct {
	ID        string
	CompanyID string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Chat) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	c.ID = id

	return tx.QueryRowContext(ctx, `
		INSERT INTO chats (id, company_id, name)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at`,
		c.ID, c.CompanyID, c.Name,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (c *Chat) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE chats SET name = $1 WHERE id = $2
		RETURNING updated_at`, c.Name, c.ID,
	).Scan(&c.UpdatedAt)
}

func (c *Chat) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM chats WHERE id = $1", c.ID)
	return err
}

func (c *Chat) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, company_id, name, created_at, updated_at
		FROM chats WHERE id = $1`, c.ID,
	).Scan(&c.ID, &c.CompanyID, &c.Name, &c.CreatedAt, &c.UpdatedAt)
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
		SELECT id, company_id, name, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chats
		WHERE company_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3`, c.CompanyID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Chat, 0)
	var total int64
	for rows.Next() {
		var item Chat
		if err := rows.Scan(&item.ID, &item.CompanyID, &item.Name, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
