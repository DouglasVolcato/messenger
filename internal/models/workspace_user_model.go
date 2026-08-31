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

func (wu *WorkspaceUser) GetOneByWorkspaceAndUser(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, workspace_id, user_id, role, status, created_at, updated_at
		FROM workspace_users
		WHERE workspace_id = $1 AND user_id = $2`, wu.WorkspaceID, wu.UserID,
	).Scan(&wu.ID, &wu.WorkspaceID, &wu.UserID, &wu.Role, &wu.Status, &wu.CreatedAt, &wu.UpdatedAt)
}

func (wu *WorkspaceUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]WorkspaceUser, int64, error) {
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
		SELECT id, workspace_id, user_id, role, status, created_at, updated_at, COUNT(*) OVER() AS total
		FROM workspace_users
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, wu.WorkspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]WorkspaceUser, 0)
	var total int64
	for rows.Next() {
		var item WorkspaceUser
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.UserID, &item.Role, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
