package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Company struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Company) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	c.ID = id
	if c.Status == "" {
		c.Status = "ACTIVE"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO companies (id, name, status)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at`,
		c.ID, c.Name, c.Status,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (c *Company) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE companies SET name = $1, status = $2 WHERE id = $3
		RETURNING updated_at`,
		c.Name, c.Status, c.ID,
	).Scan(&c.UpdatedAt)
}

func (c *Company) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM companies WHERE id = $1", c.ID)
	return err
}

func (c *Company) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, status, created_at, updated_at
		FROM companies WHERE id = $1`, c.ID,
	).Scan(&c.ID, &c.Name, &c.Status, &c.CreatedAt, &c.UpdatedAt)
}

func (c *Company) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Company, int64, error) {
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
		SELECT id, name, status, created_at, updated_at, COUNT(*) OVER() AS total
		FROM companies
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Company, 0)
	var total int64
	for rows.Next() {
		var item Company
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
