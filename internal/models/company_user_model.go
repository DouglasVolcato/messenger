package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type CompanyUser struct {
	ID        string
	CompanyID string
	UserID    string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (cu *CompanyUser) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	cu.ID = id
	if cu.Role == "" {
		cu.Role = "MEMBER"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO company_users (id, company_id, user_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		cu.ID, cu.CompanyID, cu.UserID, cu.Role,
	).Scan(&cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *CompanyUser) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE company_users SET role = $1 WHERE id = $2
		RETURNING updated_at`, cu.Role, cu.ID,
	).Scan(&cu.UpdatedAt)
}

func (cu *CompanyUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM company_users WHERE id = $1", cu.ID)
	return err
}

func (cu *CompanyUser) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, company_id, user_id, role, created_at, updated_at
		FROM company_users WHERE id = $1`, cu.ID,
	).Scan(&cu.ID, &cu.CompanyID, &cu.UserID, &cu.Role, &cu.CreatedAt, &cu.UpdatedAt)
}
