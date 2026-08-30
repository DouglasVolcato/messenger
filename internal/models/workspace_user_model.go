package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type WorkspaceUser struct {
	ID          string
	WorkspaceID string
	UserID      string
	Role        string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (wu *WorkspaceUser) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	wu.ID = id
	if wu.Role == "" {
		wu.Role = "MEMBER"
	}
	if wu.Status == "" {
		wu.Status = "ACTIVE"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO workspace_users (id, workspace_id, user_id, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		wu.ID, wu.WorkspaceID, wu.UserID, wu.Role, wu.Status,
	).Scan(&wu.CreatedAt, &wu.UpdatedAt)
}

func (wu *WorkspaceUser) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE workspace_users SET role = $1, status = $2 WHERE id = $3
		RETURNING updated_at`, wu.Role, wu.Status, wu.ID,
	).Scan(&wu.UpdatedAt)
}

func (wu *WorkspaceUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM workspace_users WHERE id = $1", wu.ID)
	return err
}

func (wu *WorkspaceUser) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, workspace_id, user_id, role, status, created_at, updated_at
		FROM workspace_users WHERE id = $1`, wu.ID,
	).Scan(&wu.ID, &wu.WorkspaceID, &wu.UserID, &wu.Role, &wu.Status, &wu.CreatedAt, &wu.UpdatedAt)
}
