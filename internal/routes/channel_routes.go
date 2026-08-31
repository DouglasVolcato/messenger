package routes

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterChannelRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /workspaces/{workspaceID}/channels", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		workspaceID := r.PathValue("workspaceID")
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE'
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'
			  AND wu.role IN ('OWNER', 'ADMIN')`, workspaceID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		offset := (page - 1) * limit

		workspace := models.Workspace{ID: workspaceID}
		if err := workspace.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT c.id, c.workspace_id, c.name, COALESCE(c.description, ''), c.type,
			       COUNT(cu.id) AS member_count, COUNT(*) OVER() AS total
			FROM channels c
			LEFT JOIN channel_users cu ON cu.channel_id = c.id
			WHERE c.workspace_id = $1
			GROUP BY c.id, c.workspace_id, c.name, c.description, c.type, c.created_at
			ORDER BY c.created_at DESC, c.id DESC
			LIMIT $2 OFFSET $3`, workspaceID, limit, offset)
		if err != nil {
			http.Error(w, "could not load channels", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		channels := make([]ChannelView, 0)
		var total int64
		for rows.Next() {
			var item ChannelView
			if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Type, &item.MemberCount, &total); err != nil {
				http.Error(w, "could not load channels", http.StatusInternalServerError)
				return
			}
			channels = append(channels, item)
		}

		utils.ExecuteTemplate(w, templ, "channels/index_admin.html", &ViewData{
			AppVersion: appVersion,
			Workspace:  workspace,
			Role:       actorRole,
			CanManage:  true,
			Channels:   channels,
			Page:       page,
			PrevPage:   page - 1,
			NextPage:   page + 1,
			Limit:      limit,
			Total:      total,
			HasPrev:    page > 1,
			HasNext:    int64(page*limit) < total,
		})
	}))

	mux.Handle("GET /workspaces/{workspaceID}/channels/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		workspaceID := r.PathValue("workspaceID")
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'
			  AND wu.role IN ('OWNER', 'ADMIN')`, workspaceID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		workspace := models.Workspace{ID: workspaceID}
		if err := workspace.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		utils.ExecuteTemplate(w, templ, "channels/create.html", &ViewData{AppVersion: appVersion, Workspace: workspace, Role: actorRole})
	}))

	mux.Handle("POST /api/workspaces/{workspaceID}/channels", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		workspaceID := r.PathValue("workspaceID")
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'
			  AND wu.role IN ('OWNER', 'ADMIN')`, workspaceID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		description := strings.TrimSpace(r.FormValue("channel-desc"))
		channel := models.Channel{WorkspaceID: workspaceID, Name: strings.TrimSpace(r.FormValue("channel-name")), Type: strings.ToUpper(r.FormValue("visibility"))}
		if description != "" {
			channel.Description = &description
		}
		if channel.Type == "" {
			channel.Type = "PUBLIC"
		}
		if channel.Name == "" || (channel.Type != "PUBLIC" && channel.Type != "PRIVATE") {
			http.Error(w, "invalid channel data", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := channel.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create channel", http.StatusBadRequest)
			return
		}
		channelUser := models.ChannelUser{ChannelID: channel.ID, UserID: session.ID, Role: "OWNER"}
		if err := channelUser.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create channel membership", http.StatusInternalServerError)
			return
		}
		chatName := channel.Name
		chat := models.Chat{WorkspaceID: workspaceID, ChannelID: &channel.ID, Type: "CHANNEL", Name: &chatName}
		if err := chat.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create channel chat", http.StatusInternalServerError)
			return
		}
		chatUser := models.ChatUser{ChatID: chat.ID, UserID: session.ID}
		if err := chatUser.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create chat membership", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create channel", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"/channels")
	}))

	mux.Handle("GET /channels/{channelID}/edit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		utils.ExecuteTemplate(w, templ, "channels/edit.html", &ViewData{AppVersion: appVersion, Channel: channel, Role: workspaceRole, CanManage: true})
	}))

	mux.Handle("POST /api/channels/{channelID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		channel.Name = strings.TrimSpace(r.FormValue("channel-name"))
		if channel.Name == "" {
			http.Error(w, "channel name is required", http.StatusBadRequest)
			return
		}
		description := strings.TrimSpace(r.FormValue("channel-desc"))
		if description == "" {
			channel.Description = nil
		} else {
			channel.Description = &description
		}
		if visibility := strings.ToUpper(r.FormValue("visibility")); visibility != "" {
			if workspaceRole != "OWNER" && channelRole != "OWNER" {
				http.Error(w, "only an owner can change channel visibility", http.StatusForbidden)
				return
			}
			if visibility != "PUBLIC" && visibility != "PRIVATE" {
				http.Error(w, "invalid channel visibility", http.StatusBadRequest)
				return
			}
			channel.Type = visibility
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := channel.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update channel", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update channel", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+channel.WorkspaceID+"/channels")
	}))

	mux.Handle("GET /channels/{channelID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		canManage := workspaceRole == "OWNER" || workspaceRole == "ADMIN" || channelRole == "OWNER" || channelRole == "ADMIN"
		if channel.Type == "PRIVATE" && channelRole == "" && !canManage {
			http.NotFound(w, r)
			return
		}
		if workspaceRole == "GUEST" && channelRole == "" {
			http.NotFound(w, r)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		offset := (page - 1) * limit
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, cu.role, 'ACTIVE', COUNT(*) OVER()
			FROM channel_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.channel_id = $1
			ORDER BY cu.created_at ASC, u.name ASC
			LIMIT $2 OFFSET $3`, channel.ID, limit, offset)
		if err != nil {
			http.Error(w, "could not load members", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		members := make([]MemberView, 0)
		var total int64
		for rows.Next() {
			var member MemberView
			if err := rows.Scan(&member.ID, &member.UserID, &member.Name, &member.Username, &member.Email, &member.Role, &member.Status, &total); err != nil {
				http.Error(w, "could not load members", http.StatusInternalServerError)
				return
			}
			members = append(members, member)
		}
		utils.ExecuteTemplate(w, templ, "channels/members/index.html", &ViewData{AppVersion: appVersion, Channel: channel, Role: workspaceRole, CanManage: canManage, Members: members, Page: page, Limit: limit, Total: total, PrevPage: page - 1, NextPage: page + 1, HasPrev: page > 1, HasNext: int64(page*limit) < total})
	}))

	mux.Handle("GET /channels/{channelID}/members/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		utils.ExecuteTemplate(w, templ, "channels/members/create.html", &ViewData{AppVersion: appVersion, Channel: channel, Role: workspaceRole, CanManage: true})
	}))

	mux.Handle("POST /api/channels/{channelID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		identifier := strings.TrimSpace(r.FormValue("identifier"))
		target := models.User{Email: identifier}
		err = target.GetOneByEmail(db.DB, r.Context())
		if err == sql.ErrNoRows {
			target = models.User{Username: identifier}
			err = target.GetOneByUsername(db.DB, r.Context())
		}
		if err != nil || target.Status != "ACTIVE" {
			http.Error(w, "active user not found", http.StatusNotFound)
			return
		}
		var workspaceMembershipID string
		if err := db.DB.QueryRowContext(r.Context(), `SELECT id FROM workspace_users WHERE workspace_id = $1 AND user_id = $2 AND status = 'ACTIVE'`, channel.WorkspaceID, target.ID).Scan(&workspaceMembershipID); err != nil {
			http.Error(w, "user must belong to the workspace first", http.StatusBadRequest)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		if role == "" {
			role = "MEMBER"
		}
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if role == "OWNER" && workspaceRole != "OWNER" && channelRole != "OWNER" {
			http.Error(w, "only an owner can assign channel owner", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		membership := models.ChannelUser{ChannelID: channel.ID, UserID: target.ID, Role: role}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "user is already in this channel", http.StatusBadRequest)
			return
		}
		var chatID string
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM chats WHERE channel_id = $1`, channel.ID).Scan(&chatID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "channel chat not found", http.StatusInternalServerError)
			return
		}
		var existingChatUserID string
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2`, chatID, target.ID).Scan(&existingChatUserID)
		if err == sql.ErrNoRows {
			chatUser := models.ChatUser{ChatID: chatID, UserID: target.ID}
			if err := chatUser.Create(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not add chat membership", http.StatusInternalServerError)
				return
			}
		} else if err == nil {
			if _, err := tx.ExecContext(r.Context(), `UPDATE chat_users SET left_at = NULL WHERE id = $1`, existingChatUserID); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not restore chat membership", http.StatusInternalServerError)
				return
			}
		} else {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not check chat membership", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/channels/"+channel.ID+"/members")
	}))

	mux.Handle("GET /channels/{channelID}/members/{userID}/edit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var member MemberView
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, cu.role, 'ACTIVE'
			FROM channel_users cu JOIN users u ON u.id = cu.user_id
			WHERE cu.channel_id = $1 AND cu.user_id = $2`, channel.ID, r.PathValue("userID")).Scan(&member.ID, &member.UserID, &member.Name, &member.Username, &member.Email, &member.Role, &member.Status)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if member.Role == "OWNER" && workspaceRole != "OWNER" && channelRole != "OWNER" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		utils.ExecuteTemplate(w, templ, "channels/members/edit.html", &ViewData{AppVersion: appVersion, Channel: channel, Role: workspaceRole, CanManage: true, Member: member})
	}))

	mux.Handle("POST /api/channels/{channelID}/members/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		membership := models.ChannelUser{ChannelID: channel.ID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByChannelAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		if role == "" {
			role = membership.Role
		}
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if (membership.Role == "OWNER" || role == "OWNER") && workspaceRole != "OWNER" && channelRole != "OWNER" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE channel_users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock channel memberships", http.StatusInternalServerError)
			return
		}
		if membership.Role == "OWNER" && role != "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM channel_users WHERE channel_id = $1 AND role = 'OWNER'`, channel.ID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last channel owner cannot be demoted", http.StatusBadRequest)
				return
			}
		}
		membership.Role = role
		if err := membership.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update member", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/channels/"+channel.ID+"/members")
	}))

	mux.Handle("POST /api/channels/{channelID}/members/{userID}/remove", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		channel := models.Channel{ID: r.PathValue("channelID")}
		if err := channel.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceRole, channelRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, COALESCE(cu.role, '')
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			LEFT JOIN channel_users cu ON cu.channel_id = $3 AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, channel.WorkspaceID, session.ID, channel.ID).Scan(&workspaceRole, &channelRole); err != nil || ((workspaceRole != "OWNER" && workspaceRole != "ADMIN") && (channelRole != "OWNER" && channelRole != "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		membership := models.ChannelUser{ChannelID: channel.ID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByChannelAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		if membership.Role == "OWNER" && workspaceRole != "OWNER" && channelRole != "OWNER" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE channel_users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock channel memberships", http.StatusInternalServerError)
			return
		}
		if membership.Role == "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM channel_users WHERE channel_id = $1 AND role = 'OWNER'`, channel.ID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last channel owner cannot be removed", http.StatusBadRequest)
				return
			}
		}
		if err := membership.Delete(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove member", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM chat_users WHERE user_id = $1 AND chat_id = (SELECT id FROM chats WHERE channel_id = $2)`, membership.UserID, channel.ID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove chat membership", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not remove member", http.StatusInternalServerError)
			return
		}
		if membership.UserID == session.ID {
			utils.Redirect(w, r, "/workspaces/"+channel.WorkspaceID)
			return
		}
		utils.Redirect(w, r, "/channels/"+channel.ID+"/members")
	}))
}
