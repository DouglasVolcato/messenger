package models

import (
	"context"
	"database/sql"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 100
)

func pagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return limit, (page - 1) * limit
}

func (u *User) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]User, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, username, email, password_hash, status, created_at, updated_at,
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
			&item.Status, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		users = append(users, item)
	}
	return users, total, rows.Err()
}

func (c *Company) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Company, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, status, created_at, updated_at, COUNT(*) OVER() AS total
		FROM companies
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Company, 0)
	var total int64
	for rows.Next() {
		var item Company
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (cu *CompanyUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]CompanyUser, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, company_id, user_id, role, created_at, updated_at, COUNT(*) OVER() AS total
		FROM company_users
		WHERE company_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, cu.CompanyID, limit, offset)
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

func (w *Workspace) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Workspace, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, company_id, name, slug, status, created_at, updated_at, COUNT(*) OVER() AS total
		FROM workspaces
		WHERE company_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, w.CompanyID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Workspace, 0)
	var total int64
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.ID, &item.CompanyID, &item.Name, &item.Slug, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (wu *WorkspaceUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]WorkspaceUser, int64, error) {
	limit, offset := pagination(page, limit)
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

func (c *Channel) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Channel, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, workspace_id, name, description, type, created_at, updated_at, COUNT(*) OVER() AS total
		FROM channels
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, c.WorkspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Channel, 0)
	var total int64
	for rows.Next() {
		var item Channel
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Type, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (cu *ChannelUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChannelUser, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, channel_id, user_id, role, created_at, updated_at, COUNT(*) OVER() AS total
		FROM channel_users
		WHERE channel_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, cu.ChannelID, limit, offset)
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

func (c *Chat) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]Chat, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, workspace_id, channel_id, type, name, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chats
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, c.WorkspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Chat, 0)
	var total int64
	for rows.Next() {
		var item Chat
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.ChannelID, &item.Type, &item.Name, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (cu *ChatUser) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatUser, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_id, user_id, last_read_message_id, joined_at, left_at, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM chat_users
		WHERE chat_id = $1
		ORDER BY joined_at DESC, id DESC
		LIMIT $2 OFFSET $3`, cu.ChatID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatUser, 0)
	var total int64
	for rows.Next() {
		var item ChatUser
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.UserID, &item.LastReadMessageID, &item.JoinedAt,
			&item.LeftAt, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (m *ChatMessage) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatMessage, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_id, user_id, client_message_id, sequence, type, content,
		       edited_at, deleted_at, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chat_messages
		WHERE chat_id = $1
		ORDER BY sequence DESC
		LIMIT $2 OFFSET $3`, m.ChatID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatMessage, 0)
	var total int64
	for rows.Next() {
		var item ChatMessage
		if err := rows.Scan(
			&item.ID, &item.ChatID, &item.UserID, &item.ClientMessageID, &item.Sequence,
			&item.Type, &item.Content, &item.EditedAt, &item.DeletedAt, &item.CreatedAt,
			&item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *ChatMessageReaction) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]ChatMessageReaction, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, chat_message_id, user_id, reaction, created_at, updated_at, COUNT(*) OVER() AS total
		FROM chat_messages_reactions
		WHERE chat_message_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, r.ChatMessageID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ChatMessageReaction, 0)
	var total int64
	for rows.Next() {
		var item ChatMessageReaction
		if err := rows.Scan(&item.ID, &item.ChatMessageID, &item.UserID, &item.Reaction, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (n *UserNotification) GetMany(db *sql.DB, ctx context.Context, page, limit int) ([]UserNotification, int64, error) {
	limit, offset := pagination(page, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, type, title, content, is_read, read_at, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM user_notifications
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, n.UserID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]UserNotification, 0)
	var total int64
	for rows.Next() {
		var item UserNotification
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Type, &item.Title, &item.Content, &item.IsRead,
			&item.ReadAt, &item.CreatedAt, &item.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
