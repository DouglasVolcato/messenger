package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type Workspace struct {
	ID        string
	CompanyID string
	Name      string
	Slug      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (w *Workspace) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	w.ID = id
	if w.Status == "" {
		w.Status = "ACTIVE"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO workspaces (id, company_id, name, slug, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		w.ID, w.CompanyID, w.Name, w.Slug, w.Status,
	).Scan(&w.CreatedAt, &w.UpdatedAt)
}

func (w *Workspace) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE workspaces SET name = $1, slug = $2, status = $3 WHERE id = $4
		RETURNING updated_at`, w.Name, w.Slug, w.Status, w.ID,
	).Scan(&w.UpdatedAt)
}

func (w *Workspace) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM workspaces WHERE id = $1", w.ID)
	return err
}

func (w *Workspace) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, company_id, name, slug, status, created_at, updated_at
		FROM workspaces WHERE id = $1`, w.ID,
	).Scan(&w.ID, &w.CompanyID, &w.Name, &w.Slug, &w.Status, &w.CreatedAt, &w.UpdatedAt)
}
