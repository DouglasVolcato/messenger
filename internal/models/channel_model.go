package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Channel struct {
	ID          string
	WorkspaceID string
	Name        string
	Description *string
	Type        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (c *Channel) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	c.ID = id
	if c.Type == "" {
		c.Type = "PUBLIC"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO channels (id, workspace_id, name, description, type)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		c.ID, c.WorkspaceID, c.Name, c.Description, c.Type,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (c *Channel) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE channels SET name = $1, description = $2, type = $3 WHERE id = $4
		RETURNING updated_at`, c.Name, c.Description, c.Type, c.ID,
	).Scan(&c.UpdatedAt)
}

func (c *Channel) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM channels WHERE id = $1", c.ID)
	return err
}

func (c *Channel) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, description, type, created_at, updated_at
		FROM channels WHERE id = $1`, c.ID,
	).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Description, &c.Type, &c.CreatedAt, &c.UpdatedAt)
}
