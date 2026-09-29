package routes

import (
	"database/sql"
	"html/template"
	"net/http"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/lib/pq"
)

func loadCompanyChats(r *http.Request, companyID, userID string) ([]ChatView, error) {
	key := companyChatsCacheKey(companyID)
	chats, err := cache.GetChatsCache(r.Context(), key)
	if err != nil {
		model := models.Chat{CompanyID: companyID}
		chats, _, err = model.GetMany(db.DB, r.Context(), 1, 100)
		if err != nil {
			return nil, err
		}
		_ = cache.SetChatsCache(r.Context(), chats, key)
	}
	if len(chats) == 0 {
		return []ChatView{}, nil
	}

	chatIDs := make([]string, 0, len(chats))
	for _, chat := range chats {
		chatIDs = append(chatIDs, chat.ID)
	}
	rows, err := db.DB.QueryContext(r.Context(), `
		SELECT ch.id, ch.company_id, ch.name,
		       EXISTS(SELECT 1 FROM chat_users mine WHERE mine.chat_id = ch.id AND mine.user_id = $2),
		       (SELECT COUNT(*) FROM chat_users members WHERE members.chat_id = ch.id)
		FROM chats ch
		WHERE ch.id::text = ANY($1)
		ORDER BY ch.created_at ASC, ch.id ASC`, pq.Array(chatIDs), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := make([]ChatView, 0, len(chats))
	for rows.Next() {
		var view ChatView
		if err := rows.Scan(&view.ID, &view.CompanyID, &view.Name, &view.Subscribed, &view.MemberCount); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func loadCompanyMembers(r *http.Request, companyID string) ([]MemberView, error) {
	users, err := cache.GetCompanyUsersCache(r.Context(), companyID)
	if err != nil {
		rows, queryErr := db.DB.QueryContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, u.password_hash, u.status, u.created_at, u.updated_at, cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1
			ORDER BY CASE WHEN cu.role = 'ADMIN' THEN 0 ELSE 1 END, u.name ASC`, companyID)
		if queryErr != nil {
			return nil, queryErr
		}
		defer rows.Close()
		users = make([]models.User, 0)
		members := make([]MemberView, 0)
		for rows.Next() {
			var user models.User
			var member MemberView
			if scanErr := rows.Scan(&member.ID, &user.ID, &user.Name, &user.Username, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt, &member.Role); scanErr != nil {
				return nil, scanErr
			}
			member.UserID = user.ID
			member.Name = user.Name
			member.Username = user.Username
			member.Email = user.Email
			member.Status = user.Status
			users = append(users, user)
			members = append(members, member)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		_ = cache.SetCompanyUsersCache(r.Context(), users, companyID)
		return members, nil
	}

	roles := make(map[string]MemberView, len(users))
	rows, err := db.DB.QueryContext(r.Context(), `
		SELECT id, user_id, role
		FROM company_users
		WHERE company_id = $1`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var member MemberView
		if err := rows.Scan(&member.ID, &member.UserID, &member.Role); err != nil {
			return nil, err
		}
		roles[member.UserID] = member
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	members := make([]MemberView, 0, len(users))
	for _, user := range users {
		member, ok := roles[user.ID]
		if !ok {
			continue
		}
		member.Name, member.Username, member.Email, member.Status = user.Name, user.Username, user.Email, user.Status
		members = append(members, member)
	}
	return members, nil
}

func RegisterCompanyRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /companies", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}

		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT c.id, c.name, c.status, cu.role
			FROM company_users cu
			JOIN companies c ON c.id = cu.company_id
			WHERE cu.user_id = $1
			ORDER BY c.name ASC, c.id ASC`, user.ID)
		if err != nil {
			http.Error(w, "could not load companies", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		companies := make([]CompanyView, 0)
		for rows.Next() {
			var item CompanyView
			if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Role); err != nil {
				http.Error(w, "could not load companies", http.StatusInternalServerError)
				return
			}
			item.CanManage = item.Role == "ADMIN"
			companies = append(companies, item)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "could not load companies", http.StatusInternalServerError)
			return
		}

		utils.ExecuteTemplate(w, templ, "companies/index.html", &ViewData{
			AppVersion:          appVersion,
			User:                user,
			Companies:           companies,
			UnreadNotifications: unreadNotifications(user.ID, r),
			Success:             r.URL.Query().Get("success"),
			Error:               r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("GET /companies/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		utils.ExecuteTemplate(w, templ, "companies/create.html", &ViewData{
			AppVersion: appVersion,
			User:       user,
			BackURL:    "/companies",
			Error:      r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/companies", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			utils.Redirect(w, r, "/companies/new?error=Company+name+is+required")
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		company := models.Company{Name: name, Status: "ACTIVE"}
		if err := company.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create company", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: company.ID, UserID: user.ID, Role: "ADMIN"}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create company membership", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create company", http.StatusInternalServerError)
			return
		}
		_ = cache.SetCompanyCache(r.Context(), company)
		_ = cache.DeleteCompanyUsersCache(r.Context(), company.ID)
		utils.Redirect(w, r, "/companies/"+company.ID+"?success=Company+created")
	}))

	mux.Handle("GET /companies/{companyID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		company, role, ok := requireCompanyMember(w, r, companyID, user.ID)
		if !ok {
			return
		}

		chats, err := loadCompanyChats(r, companyID, user.ID)
		if err != nil {
			http.Error(w, "could not load chats", http.StatusInternalServerError)
			return
		}

		members, err := loadCompanyMembers(r, companyID)
		if err != nil {
			http.Error(w, "could not load company users", http.StatusInternalServerError)
			return
		}

		utils.ExecuteTemplate(w, templ, "companies/show.html", &ViewData{
			AppVersion:          appVersion,
			User:                user,
			Company:             company,
			CompanyRole:         role,
			CanManage:           role == "ADMIN",
			Chats:               chats,
			Members:             members,
			UnreadNotifications: unreadNotifications(user.ID, r),
			Success:             r.URL.Query().Get("success"),
			Error:               r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("GET /companies/{companyID}/settings", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		company, ok := requireCompanyAdmin(w, r, r.PathValue("companyID"), user.ID)
		if !ok {
			return
		}
		utils.ExecuteTemplate(w, templ, "companies/settings.html", &ViewData{
			AppVersion:  appVersion,
			User:        user,
			Company:     company,
			CompanyRole: "ADMIN",
			CanManage:   true,
			BackURL:     "/companies/" + company.ID,
			Success:     r.URL.Query().Get("success"),
			Error:       r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/companies/{companyID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		company, ok := requireCompanyAdmin(w, r, companyID, user.ID)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		company.Name = strings.TrimSpace(r.FormValue("name"))
		if company.Name == "" {
			utils.Redirect(w, r, "/companies/"+companyID+"/settings?error=Company+name+is+required")
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := company.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update company", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update company", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteCompanyCache(r.Context(), companyID)
		utils.Redirect(w, r, "/companies/"+companyID+"/settings?success=Company+updated")
	}))

	mux.Handle("GET /companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		company, ok := requireCompanyAdmin(w, r, r.PathValue("companyID"), user.ID)
		if !ok {
			return
		}
		members, err := loadCompanyMembers(r, company.ID)
		if err != nil {
			http.Error(w, "could not load members", http.StatusInternalServerError)
			return
		}
		utils.ExecuteTemplate(w, templ, "companies/members/index.html", &ViewData{
			AppVersion:  appVersion,
			User:        user,
			Company:     company,
			CompanyRole: "ADMIN",
			CanManage:   true,
			Members:     members,
			BackURL:     "/companies/" + company.ID,
			Success:     r.URL.Query().Get("success"),
			Error:       r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, ok := requireCompanyAdmin(w, r, companyID, user.ID); !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		identifier := strings.TrimSpace(r.FormValue("identifier"))
		target := models.User{Email: identifier}
		err := target.GetOneByEmail(db.DB, r.Context())
		if err == sql.ErrNoRows {
			target = models.User{Username: identifier}
			err = target.GetOneByUsername(db.DB, r.Context())
		}
		if err != nil || target.Status != "ACTIVE" {
			utils.Redirect(w, r, "/companies/"+companyID+"/members?error=Active+user+not+found")
			return
		}

		role := strings.ToUpper(strings.TrimSpace(r.FormValue("role")))
		if role == "" {
			role = "USER"
		}
		if role != "USER" && role != "ADMIN" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: target.ID, Role: role}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			utils.Redirect(w, r, "/companies/"+companyID+"/members?error=User+already+belongs+to+this+company")
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add user", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteCompanyUsersCache(r.Context(), companyID)
		utils.Redirect(w, r, "/companies/"+companyID+"/members?success=User+added")
	}))

	mux.Handle("POST /api/companies/{companyID}/members/{userID}/role", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, ok := requireCompanyAdmin(w, r, companyID, user.ID); !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		targetUserID := r.PathValue("userID")
		role := strings.ToUpper(strings.TrimSpace(r.FormValue("role")))
		if role != "USER" && role != "ADMIN" {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		var lockedCompanyID string
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM companies WHERE id = $1 FOR UPDATE`, companyID).Scan(&lockedCompanyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock company", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: targetUserID}
		if err := tx.QueryRowContext(r.Context(), `
			SELECT id, role FROM company_users
			WHERE company_id = $1 AND user_id = $2
			FOR UPDATE`, companyID, targetUserID).Scan(&membership.ID, &membership.Role); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		if membership.Role == "ADMIN" && role != "ADMIN" {
			var admins int
			if err := tx.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM company_users
				WHERE company_id = $1 AND role = 'ADMIN'`, companyID).Scan(&admins); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not validate administrators", http.StatusInternalServerError)
				return
			}
			if admins <= 1 {
				_ = db.RollbackTransaction(tx)
				utils.Redirect(w, r, "/companies/"+companyID+"/members?error=Company+must+have+at+least+one+admin")
				return
			}
		}
		membership.Role = role
		if err := membership.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update user role", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update user role", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteCompanyUsersCache(r.Context(), companyID)
		utils.Redirect(w, r, "/companies/"+companyID+"/members?success=Role+updated")
	}))

	mux.Handle("POST /api/companies/{companyID}/members/{userID}/delete", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, ok := requireCompanyAdmin(w, r, companyID, user.ID); !ok {
			return
		}
		targetUserID := r.PathValue("userID")
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		var lockedCompanyID string
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM companies WHERE id = $1 FOR UPDATE`, companyID).Scan(&lockedCompanyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock company", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: targetUserID}
		if err := tx.QueryRowContext(r.Context(), `
			SELECT id, role FROM company_users
			WHERE company_id = $1 AND user_id = $2
			FOR UPDATE`, companyID, targetUserID).Scan(&membership.ID, &membership.Role); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		if membership.Role == "ADMIN" {
			var admins int
			if err := tx.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM company_users
				WHERE company_id = $1 AND role = 'ADMIN'`, companyID).Scan(&admins); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not validate administrators", http.StatusInternalServerError)
				return
			}
			if admins <= 1 {
				_ = db.RollbackTransaction(tx)
				utils.Redirect(w, r, "/companies/"+companyID+"/members?error=Company+must+have+at+least+one+admin")
				return
			}
		}
		removedChatIDs := make([]string, 0)
		rows, err := tx.QueryContext(r.Context(), `
			DELETE FROM chat_users cu
			USING chats ch
			WHERE cu.chat_id = ch.id
			  AND ch.company_id = $1
			  AND cu.user_id = $2
			RETURNING cu.chat_id`, companyID, targetUserID)
		if err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove chat subscriptions", http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var chatID string
			if err := rows.Scan(&chatID); err != nil {
				rows.Close()
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not remove chat subscriptions", http.StatusInternalServerError)
				return
			}
			removedChatIDs = append(removedChatIDs, chatID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove chat subscriptions", http.StatusInternalServerError)
			return
		}
		rows.Close()
		if err := membership.Delete(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove user", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not remove user", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteCompanyUsersCache(r.Context(), companyID)
		_ = cache.DeleteChatMembersCaches(r.Context(), removedChatIDs)
		utils.Redirect(w, r, "/companies/"+companyID+"/members?success=User+removed")
	}))

	mux.Handle("GET /companies/{companyID}/chats/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		company, ok := requireCompanyAdmin(w, r, r.PathValue("companyID"), user.ID)
		if !ok {
			return
		}
		utils.ExecuteTemplate(w, templ, "companies/chats/create.html", &ViewData{
			AppVersion:  appVersion,
			User:        user,
			Company:     company,
			CompanyRole: "ADMIN",
			CanManage:   true,
			BackURL:     "/companies/" + company.ID,
			Error:       r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/companies/{companyID}/chats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, ok := requireCompanyAdmin(w, r, companyID, user.ID); !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			utils.Redirect(w, r, "/companies/"+companyID+"/chats/new?error=Chat+name+is+required")
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		chat := models.Chat{CompanyID: companyID, Name: name}
		if err := chat.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create chat", http.StatusInternalServerError)
			return
		}
		subscription := models.ChatUser{ChatID: chat.ID, UserID: user.ID}
		if err := subscription.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not subscribe to chat", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create chat", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteChatsCache(r.Context(), companyChatsCacheKey(companyID))
		utils.Redirect(w, r, "/companies/"+companyID+"/chats/"+chat.ID)
	}))

	mux.Handle("POST /api/companies/{companyID}/chats/{chatID}/subscribe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, _, ok := requireCompanyMember(w, r, companyID, user.ID); !ok {
			return
		}
		chatID := r.PathValue("chatID")
		var validChatID string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT id FROM chats WHERE id = $1 AND company_id = $2`,
			chatID, companyID,
		).Scan(&validChatID); err != nil {
			http.NotFound(w, r)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		subscription := models.ChatUser{ChatID: chatID, UserID: user.ID}
		if err := subscription.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not subscribe", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not subscribe", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteChatMembersCache(r.Context(), chatID)
		utils.Redirect(w, r, "/companies/"+companyID+"/chats/"+chatID)
	}))

	mux.Handle("POST /api/companies/{companyID}/chats/{chatID}/unsubscribe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		if _, _, ok := requireCompanyMember(w, r, companyID, user.ID); !ok {
			return
		}
		chatID := r.PathValue("chatID")
		var validChatID string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT id FROM chats WHERE id = $1 AND company_id = $2`,
			chatID, companyID,
		).Scan(&validChatID); err != nil {
			http.NotFound(w, r)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		subscription := models.ChatUser{ChatID: chatID, UserID: user.ID}
		if err := subscription.Delete(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not unsubscribe", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not unsubscribe", http.StatusInternalServerError)
			return
		}
		_ = cache.DeleteChatMembersCache(r.Context(), chatID)
		utils.Redirect(w, r, "/companies/"+companyID+"?success=Chat+subscription+removed")
	}))
}
