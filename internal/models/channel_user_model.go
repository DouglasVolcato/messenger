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
	var existingMembers int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_users WHERE channel_id = $1`, cu.ChannelID).Scan(&existingMembers)
	if err := tx.QueryRowContext(ctx, `INSERT INTO channel_users (id, channel_id, user_id, role) VALUES ($1,$2,$3,$4) RETURNING created_at, updated_at`, cu.ID, cu.ChannelID, cu.UserID, cu.Role).Scan(&cu.CreatedAt, &cu.UpdatedAt); err != nil {
		return err
	}
	if existingMembers > 0 {
		var channelName, workspaceID string
		if err := tx.QueryRowContext(ctx, `SELECT name, workspace_id FROM channels WHERE id = $1`, cu.ChannelID).Scan(&channelName, &workspaceID); err == nil {
			title := "Channel access"
			actionURL := "/workspaces/" + workspaceID + "?channel=" + cu.ChannelID
			n := UserNotification{UserID: cu.UserID, Type: "CHANNEL_MEMBERSHIP", Title: &title, Content: "You were added to #" + channelName + ".", ActionURL: &actionURL}
			if err := n.Create(tx, ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
func (cu *ChannelUser) Update(tx *sql.Tx, ctx context.Context) error {
	return tx.QueryRowContext(ctx, `UPDATE channel_users SET role=$1 WHERE id=$2 RETURNING updated_at`, cu.Role, cu.ID).Scan(&cu.UpdatedAt)
}
func (cu *ChannelUser) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM channel_users WHERE id = $1", cu.ID)
	return err
}
func (cu *ChannelUser) GetOne(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `SELECT id, channel_id, user_id, role, created_at, updated_at FROM channel_users WHERE id=$1`, cu.ID).Scan(&cu.ID, &cu.ChannelID, &cu.UserID, &cu.Role, &cu.CreatedAt, &cu.UpdatedAt)
}
func (cu *ChannelUser) GetOneByChannelAndUser(db *sql.DB, ctx context.Context) error {
	return db.QueryRowContext(ctx, `SELECT id, channel_id, user_id, role, created_at, updated_at FROM channel_users WHERE channel_id=$1 AND user_id=$2`, cu.ChannelID, cu.UserID).Scan(&cu.ID, &cu.ChannelID, &cu.UserID, &cu.Role, &cu.CreatedAt, &cu.UpdatedAt)
}
func (cu *ChannelUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChannelUser, int64, error) {
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
	rows, err := db.QueryContext(ctx, `SELECT id, channel_id, user_id, role, created_at, updated_at, COUNT(*) OVER() AS total FROM channel_users WHERE channel_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, cu.ChannelID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]ChannelUser, 0)
	var total int64
	for rows.Next() {
		var item ChannelUser
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.UserID, &item.Role, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
