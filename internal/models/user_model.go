package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type User struct {
	ID           string
	Name         string
	Username     string
	Email        string
	Password     string
	PasswordHash string
	Status       string
	SystemAdmin  bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u *User) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	u.ID = id

	if u.Status == "" {
		u.Status = "ACTIVE"
	}

	if u.Password != "" {
		u.PasswordHash, err = utils.HashPassword(u.Password)
		if err != nil {
			return err
		}
		u.Password = ""
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO users (id, name, username, email, password_hash, status, is_system_admin)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		u.ID, u.Name, u.Username, u.Email, u.PasswordHash, u.Status, u.SystemAdmin,
	).Scan(&u.CreatedAt, &u.UpdatedAt)
}

func (u *User) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE users
		SET name = $1, username = $2, email = $3, status = $4, is_system_admin = $5
		WHERE id = $6
		RETURNING updated_at`,
		u.Name, u.Username, u.Email, u.Status, u.SystemAdmin, u.ID,
	).Scan(&u.UpdatedAt)
}

func (u *User) UpdatePassword(tx *sql.Tx, ctx context.Context) error {
	if u.Password != "" {
		hashedPassword, err := utils.HashPassword(u.Password)
		if err != nil {
			return err
		}
		u.PasswordHash = hashedPassword
		u.Password = ""
	}

	return tx.QueryRowContext(ctx, `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2
		RETURNING updated_at`,
		u.PasswordHash, u.ID,
	).Scan(&u.UpdatedAt)
}

func (u *User) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = $1", u.ID)
	return err
}

func (u *User) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, username, email, password_hash, status, is_system_admin, created_at, updated_at
		FROM users
		WHERE id = $1`, u.ID,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.SystemAdmin, &u.CreatedAt, &u.UpdatedAt)
}

func (u *User) GetOneByEmail(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, username, email, password_hash, status, is_system_admin, created_at, updated_at
		FROM users
		WHERE email = $1`, u.Email,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.SystemAdmin, &u.CreatedAt, &u.UpdatedAt)
}

func (u *User) GetOneByUsername(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, username, email, password_hash, status, is_system_admin, created_at, updated_at
		FROM users
		WHERE username = $1`, u.Username,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.SystemAdmin, &u.CreatedAt, &u.UpdatedAt)
}

func (u *User) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]User, int64, error) {
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
		SELECT id, name, username, email, password_hash, status, is_system_admin, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM users
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]User, 0)
	var total int64
	for rows.Next() {
		var item User
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Username, &item.Email, &item.PasswordHash,
			&item.Status, &item.SystemAdmin, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		users = append(users, item)
	}

	return users, total, rows.Err()
}
