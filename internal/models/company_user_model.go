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

	var existingMembers int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_users WHERE company_id = $1`, cu.CompanyID).Scan(&existingMembers)

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO company_users (id, company_id, user_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		cu.ID, cu.CompanyID, cu.UserID, cu.Role,
	).Scan(&cu.CreatedAt, &cu.UpdatedAt); err != nil {
		return err
	}

	if existingMembers > 0 {
		var companyName string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM companies WHERE id = $1`, cu.CompanyID).Scan(&companyName); err == nil {
			title := "Company access"
			actionURL := "/workspaces"
			notification := UserNotification{
				UserID:    cu.UserID,
				Type:      "COMPANY_MEMBERSHIP",
				Title:     &title,
				Content:   "You were added to " + companyName + " as " + cu.Role + ".",
				ActionURL: &actionURL,
			}
			if err := notification.Create(tx, ctx); err != nil {
				return err
			}
		}
	}
	return nil
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

func (cu *CompanyUser) GetOneByCompanyAndUser(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, company_id, user_id, role, created_at, updated_at
		FROM company_users
		WHERE company_id = $1 AND user_id = $2`, cu.CompanyID, cu.UserID,
	).Scan(&cu.ID, &cu.CompanyID, &cu.UserID, &cu.Role, &cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *CompanyUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]CompanyUser, int64, error) {
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
		SELECT id, company_id, user_id, role, created_at, updated_at, COUNT(*) OVER() AS total
		FROM company_users WHERE company_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, cu.CompanyID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]CompanyUser, 0)
	var total int64
	for rows.Next() {
		var item CompanyUser
		if err := rows.Scan(&item.ID, &item.CompanyID, &item.UserID, &item.Role, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
