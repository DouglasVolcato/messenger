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
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		currentUser := models.User{ID: session.ID}
		if err := currentUser.GetOne(db.DB, r.Context()); err != nil || currentUser.Status != "ACTIVE" {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}

		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT c.id, c.name, c.status, cu.role
			FROM company_users cu
			JOIN companies c ON c.id = cu.company_id
			WHERE cu.user_id = $1
			  AND c.status = 'ACTIVE'
			ORDER BY c.name`, currentUser.ID)
		if err != nil {
			http.Error(w, "could not load companies", http.StatusInternalServerError)
			return
		}

		companies := make([]CompanyWorkspaceView, 0)
		for rows.Next() {
			var company CompanyWorkspaceView
			if err := rows.Scan(&company.ID, &company.Name, &company.Status, &company.Role); err != nil {
				rows.Close()
				http.Error(w, "could not load companies", http.StatusInternalServerError)
				return
			}
			company.CanManage = company.Role == "OWNER" || company.Role == "ADMIN"
			company.Workspaces = make([]WorkspaceView, 0)
			companies = append(companies, company)
		}
		rows.Close()

		for i := range companies {
			workspaceRows, err := db.DB.QueryContext(r.Context(), `
				SELECT w.id, w.company_id, w.name, w.slug, w.status, wu.role
				FROM workspace_users wu
				JOIN workspaces w ON w.id = wu.workspace_id
				WHERE wu.user_id = $1
				  AND w.company_id = $2
				  AND wu.status = 'ACTIVE'
				  AND w.status = 'ACTIVE'
				ORDER BY w.name`, currentUser.ID, companies[i].ID)
			if err != nil {
				http.Error(w, "could not load workspaces", http.StatusInternalServerError)
				return
			}
			for workspaceRows.Next() {
				var workspace WorkspaceView
				if err := workspaceRows.Scan(&workspace.ID, &workspace.CompanyID, &workspace.Name, &workspace.Slug, &workspace.Status, &workspace.Role); err != nil {
					workspaceRows.Close()
					http.Error(w, "could not load workspaces", http.StatusInternalServerError)
					return
				}
				workspace.CompanyName = companies[i].Name
				workspace.CompanyRole = companies[i].Role
				companies[i].Workspaces = append(companies[i].Workspaces, workspace)
			}
			workspaceRows.Close()
		}

		utils.ExecuteTemplate(w, templ, "workspaces/select.html", &ViewData{
			AppVersion:        appVersion,
			User:              currentUser,
			CompanyWorkspaces: companies,
		})
	}))

	mux.Handle("GET /workspaces/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		companyID := r.URL.Query().Get("company")
		if companyID == "" {
			if session, err := utils.GetUserFromCookie(r); err == nil {
				companyID = session.CompanyID
			}
		}
		if companyID == "" {
			http.Error(w, "company is required", http.StatusBadRequest)
			return
		}
		utils.Redirect(w, r, "/companies/"+companyID+"/workspaces/new")
	}))

	mux.Handle("GET /companies/{companyID}/workspaces/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		companyID := r.PathValue("companyID")

		var company models.Company
		var companyRole string
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT c.id, c.name, c.status, c.created_at, c.updated_at, cu.role
			FROM companies c
			JOIN company_users cu ON cu.company_id = c.id AND cu.user_id = $2
			JOIN users u ON u.id = $2
			WHERE c.id = $1
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(
			&company.ID, &company.Name, &company.Status, &company.CreatedAt, &company.UpdatedAt, &companyRole,
		)
		if err == sql.ErrNoRows {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "could not load company", http.StatusInternalServerError)
			return
		}

		utils.ExecuteTemplate(w, templ, "workspaces/create.html", &ViewData{
			AppVersion:  appVersion,
			Company:     company,
			CompanyRole: companyRole,
		})
	}))

	mux.Handle("POST /api/workspaces", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		companyID := r.FormValue("company_id")
		if companyID == "" {
			if session, err := utils.GetUserFromCookie(r); err == nil {
				companyID = session.CompanyID
			}
		}
		if companyID == "" {
			http.Error(w, "company is required", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/api/companies/"+companyID+"/workspaces", http.StatusTemporaryRedirect)
	}))

	mux.Handle("POST /api/companies/{companyID}/workspaces", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		companyID := r.PathValue("companyID")

		var companyRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN companies c ON c.id = cu.company_id
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1
			  AND cu.user_id = $2
			  AND cu.role IN ('OWNER', 'ADMIN')
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, companyID, session.ID).Scan(&companyRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		workspace := models.Workspace{
			CompanyID: companyID,
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
		membership := models.WorkspaceUser{WorkspaceID: workspace.ID, UserID: session.ID, Role: "OWNER", Status: "ACTIVE"}
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
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		workspace := models.Workspace{ID: r.PathValue("workspaceID")}
		var companyName, role, companyRole string
		var systemAdmin bool
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT w.id, w.company_id, w.name, w.slug, w.status, w.created_at, w.updated_at,
			       c.name, wu.role, cu.role, u.is_system_admin
			FROM workspaces w
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = w.id AND wu.user_id = $2
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = $2
			JOIN users u ON u.id = $2
			WHERE w.id = $1
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND wu.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, workspace.ID, session.ID).Scan(
			&workspace.ID, &workspace.CompanyID, &workspace.Name, &workspace.Slug, &workspace.Status,
			&workspace.CreatedAt, &workspace.UpdatedAt, &companyName, &role, &companyRole, &systemAdmin,
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
			ID:          session.ID,
			Role:        role,
			WorkspaceID: workspace.ID,
			CompanyID:   workspace.CompanyID,
			CompanyRole: companyRole,
			SystemAdmin: systemAdmin,
		}); err != nil {
			http.Error(w, "could not update session", http.StatusInternalServerError)
			return
		}

		query := `
			SELECT DISTINCT c.id, c.workspace_id, c.name, COALESCE(c.description, ''), c.type
			FROM channels c
			LEFT JOIN channel_users chu ON chu.channel_id = c.id AND chu.user_id = $2
			WHERE c.workspace_id = $1`
		if role == "GUEST" {
			query += ` AND chu.user_id IS NOT NULL`
		} else {
			query += ` AND (c.type = 'PUBLIC' OR chu.user_id IS NOT NULL)`
		}
		query += ` ORDER BY c.name LIMIT 100`

		rows, err := db.DB.QueryContext(r.Context(), query, workspace.ID, session.ID)
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
			LIMIT 50`, workspace.ID, session.ID)
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
			User:        models.User{ID: session.ID, SystemAdmin: systemAdmin},
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
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE'
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
			SELECT wu.id, u.id, u.name, u.username, u.email, wu.role, wu.status, COUNT(*) OVER()
			FROM workspace_users wu
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1
			  AND wu.status <> 'REMOVED'
			ORDER BY wu.created_at ASC, u.name ASC
			LIMIT $2 OFFSET $3`, workspaceID, limit, offset)
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
			Role:       actorRole,
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
		var actorRole, companyID string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.role, w.company_id
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'
			  AND wu.role IN ('OWNER', 'ADMIN')`, workspaceID, session.ID).Scan(&actorRole, &companyID); err != nil {
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

		var companyMemberID string
		if err := db.DB.QueryRowContext(r.Context(), `SELECT id FROM company_users WHERE company_id = $1 AND user_id = $2`, companyID, target.ID).Scan(&companyMemberID); err != nil {
			http.Error(w, "user must belong to the company first", http.StatusBadRequest)
			return
		}

		role := strings.ToUpper(r.FormValue("role"))
		if role == "" {
			role = "MEMBER"
		}
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" && role != "GUEST" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if actorRole == "ADMIN" && (role == "OWNER" || role == "ADMIN") {
			http.Error(w, "admins can only add members or guests", http.StatusForbidden)
			return
		}

		existing := models.WorkspaceUser{WorkspaceID: workspaceID, UserID: target.ID}
		err = existing.GetOneByWorkspaceAndUser(db.DB, r.Context())
		if err == nil && existing.Status != "REMOVED" {
			http.Error(w, "user is already in this workspace", http.StatusBadRequest)
			return
		}
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "could not check workspace membership", http.StatusInternalServerError)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if existing.ID != "" {
			existing.Role = role
			existing.Status = "ACTIVE"
			if err := existing.Update(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not restore workspace membership", http.StatusInternalServerError)
				return
			}
		} else {
			membership := models.WorkspaceUser{WorkspaceID: workspaceID, UserID: target.ID, Role: role, Status: "ACTIVE"}
			if err := membership.Create(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not add workspace member", http.StatusBadRequest)
				return
			}
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"/members")
	}))

	mux.Handle("POST /api/workspaces/{workspaceID}/members/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'
			  AND wu.role IN ('OWNER', 'ADMIN')`, workspaceID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		membership := models.WorkspaceUser{WorkspaceID: workspaceID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByWorkspaceAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		status := strings.ToUpper(r.FormValue("status"))
		if role == "" {
			role = membership.Role
		}
		if status == "" {
			status = membership.Status
		}
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" && role != "GUEST" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if status != "ACTIVE" && status != "BLOCKED" && status != "REMOVED" {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		if actorRole == "ADMIN" && (membership.Role == "OWNER" || membership.Role == "ADMIN" || role == "OWNER" || role == "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE workspace_users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock workspace memberships", http.StatusInternalServerError)
			return
		}
		if membership.Role == "OWNER" && membership.Status == "ACTIVE" && (role != "OWNER" || status != "ACTIVE") {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM workspace_users
				WHERE workspace_id = $1 AND role = 'OWNER' AND status = 'ACTIVE'`, workspaceID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last workspace owner cannot be removed or demoted", http.StatusBadRequest)
				return
			}
		}
		membership.Role = role
		membership.Status = status
		if err := membership.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update member", http.StatusBadRequest)
			return
		}
		if status == "REMOVED" {
			if _, err := tx.ExecContext(r.Context(), `
				DELETE FROM channel_users
				WHERE user_id = $1 AND channel_id IN (SELECT id FROM channels WHERE workspace_id = $2)`, membership.UserID, workspaceID); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not remove channel memberships", http.StatusInternalServerError)
				return
			}
			if _, err := tx.ExecContext(r.Context(), `
				DELETE FROM chat_users
				WHERE user_id = $1 AND chat_id IN (SELECT id FROM chats WHERE workspace_id = $2)`, membership.UserID, workspaceID); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not remove chat memberships", http.StatusInternalServerError)
				return
			}
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update member", http.StatusInternalServerError)
			return
		}
		if membership.UserID == session.ID && status != "ACTIVE" {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"/members")
	}))
}
