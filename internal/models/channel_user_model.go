package models

import (
	"context"
	"database/sql"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type ChannelUser struct {
	ID        string
	ChannelID string
	UserID    string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (cu *ChannelUser) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	cu.ID = id
	if cu.Role == "" {
		cu.Role = "MEMBER"
	}

	return tx.QueryRowContext(ctx, `
		INSERT INTO channel_users (id, channel_id, user_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		cu.ID, cu.ChannelID, cu.UserID, cu.Role,
	).Scan(&cu.CreatedAt, &cu.UpdatedAt)
}

func (cu *ChannelUser) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `
		UPDATE channel_users SET role = $1 WHERE id = $2
		RETURNING updated_at`, cu.Role, cu.ID,
	).Scan(&cu.UpdatedAt)
}

func (cu *ChannelUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM channel_users WHERE id = $1", cu.ID)
	return err
}

func (cu *ChannelUser) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `
		SELECT id, channel_id, user_id, role, created_at, updated_at
		FROM channel_users WHERE id = $1`, cu.ID,
	).Scan(&cu.ID, &cu.ChannelID, &cu.UserID, &cu.Role, &cu.CreatedAt, &cu.UpdatedAt)
}
