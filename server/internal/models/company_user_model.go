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
		cu.Role = "USER"
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO company_users (id, company_id, user_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		cu.ID, cu.CompanyID, cu.UserID, cu.Role,
	).Scan(&cu.CreatedAt, &cu.UpdatedAt); err != nil {
		return err
	}

	var companyName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM companies WHERE id = $1`, cu.CompanyID).Scan(&companyName); err == nil {
		title := "Company access"
		actionURL := "/companies/" + cu.CompanyID
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
		notificationOutbox := UserNotificationOutbox{
			UserID:    notification.UserID,
			Type:      notification.Type,
			Title:     notification.Title,
			Content:   notification.Content,
			ActionURL: notification.ActionURL,
			Status:    "PENDING",
		}
		if err := notificationOutbox.Create(tx, ctx); err != nil {
			return err
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
