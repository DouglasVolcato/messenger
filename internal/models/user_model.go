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
		INSERT INTO users (id, name, username, email, password_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`,
		u.ID, u.Name, u.Username, u.Email, u.PasswordHash, u.Status,
	).Scan(&u.CreatedAt, &u.UpdatedAt)
}

func (u *User) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE users
		SET name = $1, username = $2, email = $3, status = $4
		WHERE id = $5
		RETURNING updated_at`,
		u.Name, u.Username, u.Email, u.Status, u.ID,
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
		SELECT id, name, username, email, password_hash, status, created_at, updated_at
		FROM users
		WHERE id = $1`,
		u.ID,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
}

func (u *User) GetOneByEmail(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, username, email, password_hash, status, created_at, updated_at
		FROM users
		WHERE email = $1`,
		u.Email,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
}

func (u *User) GetOneByUsername(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, name, username, email, password_hash, status, created_at, updated_at
		FROM users
		WHERE username = $1`,
		u.Username,
	).Scan(&u.ID, &u.Name, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
}
