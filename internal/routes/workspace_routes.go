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

func RegisterWorkspaceRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /workspaces", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		rows, err := db.DB.QueryContext(r.Context(), `
            SELECT w.id, w.company_id, c.name, w.name, w.slug, w.status, wu.role, COALESCE(cu.role, '')
            FROM workspace_users wu
            JOIN workspaces w ON w.id = wu.workspace_id
            JOIN companies c ON c.id = w.company_id
            LEFT JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
            WHERE wu.user_id = $1
              AND wu.status = 'ACTIVE'
              AND w.status = 'ACTIVE'
              AND c.status = 'ACTIVE'
            ORDER BY c.name, w.name`, user.ID)
		if err != nil {
			http.Error(w, "could not load workspaces", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		items := make([]WorkspaceView, 0)
		for rows.Next() {
			var item WorkspaceView
			if err := rows.Scan(&item.ID, &item.CompanyID, &item.CompanyName, &item.Name, &item.Slug, &item.Status, &item.Role, &item.CompanyRole); err != nil {
				http.Error(w, "could not load workspaces", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}

		utils.ExecuteTemplate(w, templ, "workspaces/select.html", &ViewData{
			AppVersion: appVersion,
			Workspaces: items,
		})
	}))

	mux.Handle("GET /workspaces/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if user.CompanyID == "" || (user.CompanyRole != "OWNER" && user.CompanyRole != "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		company := models.Company{ID: user.CompanyID}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.Error(w, "company not found", http.StatusNotFound)
			return
		}
		utils.ExecuteTemplate(w, templ, "workspaces/create.html", &ViewData{
			AppVersion:  appVersion,
			Company:     company,
			CompanyRole: user.CompanyRole,
		})
	}))

	mux.Handle("POST /api/workspaces", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if user.CompanyID == "" || (user.CompanyRole != "OWNER" && user.CompanyRole != "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		workspace := models.Workspace{
			CompanyID: user.CompanyID,
			Name:      strings.TrimSpace(r.FormValue("workspace-name")),
			Slug:      strings.TrimSpace(r.FormValue("workspace-slug")),
		}
		if workspace.Name == "" || workspace.Slug == "" {
			http.Error(w, "name and slug are required", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := workspace.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create workspace", http.StatusBadRequest)
			return
		}
		membership := models.WorkspaceUser{
			WorkspaceID: workspace.ID,
			UserID:      user.ID,
			Role:        "OWNER",
			Status:      "ACTIVE",
		}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create workspace membership", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create workspace", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspace.ID)
	}))

	mux.Handle("GET /workspaces/{workspaceID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		workspace := models.Workspace{ID: r.PathValue("workspaceID")}
		var companyName, role, companyRole string
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT w.id, w.company_id, w.name, w.slug, w.status, w.created_at, w.updated_at,
			       c.name, wu.role, COALESCE(cu.role, '')
			FROM workspaces w
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = w.id AND wu.user_id = $2
			LEFT JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = $2
			WHERE w.id = $1
			  AND w.status = 'ACTIVE'
			  AND wu.status = 'ACTIVE'`, workspace.ID, user.ID).Scan(
			&workspace.ID, &workspace.CompanyID, &workspace.Name, &workspace.Slug, &workspace.Status,
			&workspace.CreatedAt, &workspace.UpdatedAt, &companyName, &role, &companyRole,
		)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not load workspace", http.StatusInternalServerError)
			return
		}

		if err := utils.SetUserCookie(w, r, utils.UserInput{
			ID:          user.ID,
			Role:        role,
			WorkspaceID: workspace.ID,
			CompanyID:   workspace.CompanyID,
			CompanyRole: companyRole,
		}); err != nil {
			http.Error(w, "could not update session", http.StatusInternalServerError)
			return
		}

		query := `
			SELECT DISTINCT c.id, c.workspace_id, c.name, COALESCE(c.description, ''), c.type
			FROM channels c
			LEFT JOIN channel_users chu ON chu.channel_id = c.id AND chu.user_id = $2
			WHERE c.workspace_id = $1`
		if role == "MEMBER" {
			query += ` AND (c.type = 'PUBLIC' OR chu.user_id IS NOT NULL)`
		}
		if role == "GUEST" {
			query += ` AND chu.user_id IS NOT NULL`
		}
		query += ` ORDER BY c.name LIMIT 100`

		rows, err := db.DB.QueryContext(r.Context(), query, workspace.ID, user.ID)
		if err != nil {
			http.Error(w, "could not load channels", http.StatusInternalServerError)
			return
		}
		channels := make([]ChannelView, 0)
		for rows.Next() {
			var channel ChannelView
			if err := rows.Scan(&channel.ID, &channel.WorkspaceID, &channel.Name, &channel.Description, &channel.Type); err != nil {
				rows.Close()
				http.Error(w, "could not load channels", http.StatusInternalServerError)
				return
			}
			channels = append(channels, channel)
		}
		rows.Close()

		chatRows, err := db.DB.QueryContext(r.Context(), `
			SELECT ch.id,
			       CASE
			           WHEN ch.type = 'DIRECT' THEN COALESCE((
			               SELECT u.name
			               FROM chat_users other_cu
			               JOIN users u ON u.id = other_cu.user_id
			               WHERE other_cu.chat_id = ch.id AND other_cu.user_id <> $2
			               LIMIT 1
			           ), 'Direct message')
			           ELSE COALESCE(ch.name, 'Group')
			       END,
			       ch.type
			FROM chat_users mine
			JOIN chats ch ON ch.id = mine.chat_id
			WHERE mine.user_id = $2
			  AND mine.left_at IS NULL
			  AND ch.workspace_id = $1
			  AND ch.type IN ('DIRECT', 'GROUP')
			ORDER BY ch.updated_at DESC
			LIMIT 50`, workspace.ID, user.ID)
		if err != nil {
			http.Error(w, "could not load chats", http.StatusInternalServerError)
			return
		}
		chats := make([]ChatView, 0)
		for chatRows.Next() {
			var chat ChatView
			if err := chatRows.Scan(&chat.ID, &chat.Name, &chat.Type); err != nil {
				chatRows.Close()
				http.Error(w, "could not load chats", http.StatusInternalServerError)
				return
			}
			chats = append(chats, chat)
		}
		chatRows.Close()

		var current ChatView
		requestedChat := r.URL.Query().Get("chat")
		for _, chat := range chats {
			if chat.ID == requestedChat {
				current = chat
				break
			}
		}
		if current.ID == "" {
			requestedChannel := r.URL.Query().Get("channel")
			if requestedChannel == "" && len(channels) > 0 {
				requestedChannel = channels[0].ID
			}
			for _, channel := range channels {
				if channel.ID != requestedChannel {
					continue
				}
				current.ChannelID = channel.ID
				current.Name = "#" + channel.Name
				current.Description = channel.Description
				current.Type = "CHANNEL"
				if err := db.DB.QueryRowContext(r.Context(), `SELECT id FROM chats WHERE channel_id = $1`, channel.ID).Scan(&current.ID); err != nil && err != sql.ErrNoRows {
					http.Error(w, "could not load channel chat", http.StatusInternalServerError)
					return
				}
				break
			}
		}
		if current.ID == "" && len(chats) > 0 {
			current = chats[0]
		}

		messages := make([]MessageView, 0)
		if current.ID != "" {
			messageRows, err := db.DB.QueryContext(r.Context(), `
				SELECT m.id, m.user_id, u.name, m.content, m.type, m.created_at, m.edited_at, m.deleted_at
				FROM chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = $1
				ORDER BY m.sequence DESC
				LIMIT 50`, current.ID)
			if err != nil {
				http.Error(w, "could not load messages", http.StatusInternalServerError)
				return
			}
			for messageRows.Next() {
				var message MessageView
				if err := messageRows.Scan(&message.ID, &message.UserID, &message.UserName, &message.Content, &message.Type, &message.CreatedAt, &message.EditedAt, &message.DeletedAt); err != nil {
					messageRows.Close()
					http.Error(w, "could not load messages", http.StatusInternalServerError)
					return
				}
				messages = append(messages, message)
			}
			messageRows.Close()
			for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
				messages[left], messages[right] = messages[right], messages[left]
			}
		}

		templateName := "workspaces/member.html"
		if role == "OWNER" || role == "ADMIN" {
			templateName = "workspaces/owner.html"
		}
		if role == "GUEST" {
			templateName = "workspaces/guest.html"
		}
		utils.ExecuteTemplate(w, templ, templateName, &ViewData{
			AppVersion:  appVersion,
			User:        models.User{ID: user.ID},
			Workspace:   workspace,
			Company:     models.Company{ID: workspace.CompanyID, Name: companyName},
			Role:        role,
			CompanyRole: companyRole,
			Channels:    channels,
			Chats:       chats,
			CurrentChat: current,
			Messages:    messages,
			CanManage:   role == "OWNER" || role == "ADMIN",
		})
	}))

	mux.Handle("GET /workspaces/{workspaceID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if user.WorkspaceID != r.PathValue("workspaceID") || (user.Role != "OWNER" && user.Role != "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		offset := (page - 1) * limit

		workspace := models.Workspace{ID: user.WorkspaceID}
		if err := workspace.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}

		rows, err := db.DB.QueryContext(r.Context(), `
            SELECT wu.id, u.id, u.name, u.username, u.email, wu.role, wu.status, COUNT(*) OVER()
            FROM workspace_users wu
            JOIN users u ON u.id = wu.user_id
            WHERE wu.workspace_id = $1
              AND wu.status <> 'REMOVED'
            ORDER BY wu.created_at ASC, u.name ASC
            LIMIT $2 OFFSET $3`, user.WorkspaceID, limit, offset)
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

		utils.ExecuteTemplate(w, templ, "workspaces/members/index.html", &ViewData{
			AppVersion: appVersion,
			Workspace:  workspace,
			Role:       user.Role,
			CanManage:  true,
			Members:    members,
			Page:       page,
			PrevPage:   page - 1,
			NextPage:   page + 1,
			Limit:      limit,
			Total:      total,
			HasPrev:    page > 1,
			HasNext:    int64(page*limit) < total,
		})
	}))

	mux.Handle("POST /api/workspaces/{workspaceID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		workspaceID := r.PathValue("workspaceID")
		if user.WorkspaceID != workspaceID || (user.Role != "OWNER" && user.Role != "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		identifier := strings.TrimSpace(r.FormValue("identifier"))
		target := models.User{Email: identifier}
		err = target.GetOneByEmail(db.DB, r.Context())
		if err == sql.ErrNoRows {
			target = models.User{Username: identifier}
			err = target.GetOneByUsername(db.DB, r.Context())
		}
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		workspace := models.Workspace{ID: workspaceID}
		if err := workspace.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var companyMemberID string
		if err := db.DB.QueryRowContext(r.Context(), `
            SELECT id FROM company_users WHERE company_id = $1 AND user_id = $2`, workspace.CompanyID, target.ID).Scan(&companyMemberID); err != nil {
			http.Error(w, "user must belong to the company first", http.StatusBadRequest)
			return
		}

		role := strings.ToUpper(r.FormValue("role"))
		if role == "" {
			role = "MEMBER"
		}
		if user.Role == "ADMIN" && (role == "OWNER" || role == "ADMIN") {
			http.Error(w, "admins can only add members or guests", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		membership := models.WorkspaceUser{WorkspaceID: workspaceID, UserID: target.ID, Role: role, Status: "ACTIVE"}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "user is already in this workspace", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"/members")
	}))

	mux.Handle("POST /api/workspaces/{workspaceID}/members/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		workspaceID := r.PathValue("workspaceID")
		if user.WorkspaceID != workspaceID || (user.Role != "OWNER" && user.Role != "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		membership := models.WorkspaceUser{WorkspaceID: workspaceID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByWorkspaceAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		status := strings.ToUpper(r.FormValue("status"))
		if user.Role == "ADMIN" && (membership.Role == "OWNER" || membership.Role == "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if user.Role == "ADMIN" && (role == "OWNER" || role == "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if membership.Role == "OWNER" && ((role != "" && role != "OWNER") || (status != "" && status != "ACTIVE")) {
			var owners int
			if err := db.DB.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM workspace_users
				WHERE workspace_id = $1 AND role = 'OWNER' AND status = 'ACTIVE'`, workspaceID).Scan(&owners); err != nil || owners <= 1 {
				http.Error(w, "the last workspace owner cannot be removed or demoted", http.StatusBadRequest)
				return
			}
		}
		if role != "" {
			membership.Role = role
		}
		if status != "" {
			membership.Status = status
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := membership.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update member", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"/members")
	}))
}
