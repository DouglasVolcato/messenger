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

func RegisterCompanyRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /companies/{companyID}/settings", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		utils.ExecuteTemplate(w, templ, "companies/settings.html", &ViewData{
			AppVersion:  appVersion,
			Company:     company,
			CompanyRole: companyRole,
			CanManage:   true,
		})
	}))

	mux.Handle("POST /api/companies/{companyID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&companyRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		company := models.Company{ID: companyID}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		company.Name = strings.TrimSpace(r.FormValue("name"))
		if company.Name == "" {
			http.Error(w, "company name is required", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := company.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update company", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update company", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/companies/"+companyID+"/settings")
	}))

	mux.Handle("GET /companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		companyID := r.PathValue("companyID")
		var companyRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&companyRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		offset := (page - 1) * limit

		company := models.Company{ID: companyID}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, cu.role, u.status, COUNT(*) OVER()
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1
			ORDER BY cu.created_at ASC, u.name ASC
			LIMIT $2 OFFSET $3`, companyID, limit, offset)
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

		utils.ExecuteTemplate(w, templ, "companies/members/index.html", &ViewData{
			AppVersion:  appVersion,
			Company:     company,
			CompanyRole: companyRole,
			CanManage:   true,
			Members:     members,
			Page:        page,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			Limit:       limit,
			Total:       total,
			HasPrev:     page > 1,
			HasNext:     int64(page*limit) < total,
		})
	}))

	mux.Handle("POST /api/companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&actorRole); err != nil {
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

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: target.ID, Role: role}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "user already belongs to this company", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/companies/"+companyID+"/members")
	}))

	mux.Handle("GET /companies/{companyID}/members/{userID}/edit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		companyID := r.PathValue("companyID")
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		company := models.Company{ID: companyID}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var member MemberView
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, cu.role, u.status
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2`, companyID, r.PathValue("userID")).Scan(
			&member.ID, &member.UserID, &member.Name, &member.Username, &member.Email, &member.Role, &member.Status,
		)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if actorRole == "ADMIN" && (member.Role == "OWNER" || member.Role == "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		utils.ExecuteTemplate(w, templ, "companies/members/edit.html", &ViewData{
			AppVersion:  appVersion,
			Company:     company,
			CompanyRole: actorRole,
			CanManage:   true,
			Member:      member,
		})
	}))

	mux.Handle("POST /api/companies/{companyID}/members/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		membership := models.CompanyUser{CompanyID: companyID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByCompanyAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		if role == "" {
			role = membership.Role
		}
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" && role != "GUEST" {
			http.Error(w, "invalid role", http.StatusBadRequest)
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
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE company_users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock company memberships", http.StatusInternalServerError)
			return
		}
		if membership.Role == "OWNER" && role != "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM company_users WHERE company_id = $1 AND role = 'OWNER'`, companyID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last owner cannot be demoted", http.StatusBadRequest)
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
		if membership.UserID == session.ID && role != "OWNER" && role != "ADMIN" {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.Redirect(w, r, "/companies/"+companyID+"/members")
	}))

	mux.Handle("POST /api/companies/{companyID}/members/{userID}/remove", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		companyID := r.PathValue("companyID")
		var actorRole string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT cu.role
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1 AND cu.user_id = $2
			  AND u.status = 'ACTIVE'
			  AND cu.role IN ('OWNER', 'ADMIN')`, companyID, session.ID).Scan(&actorRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		membership := models.CompanyUser{CompanyID: companyID, UserID: r.PathValue("userID")}
		if err := membership.GetOneByCompanyAndUser(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		if actorRole == "ADMIN" && (membership.Role == "OWNER" || membership.Role == "ADMIN") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE company_users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock company memberships", http.StatusInternalServerError)
			return
		}
		if membership.Role == "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM company_users WHERE company_id = $1 AND role = 'OWNER'`, companyID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last owner cannot be removed", http.StatusBadRequest)
				return
			}
		}
		if _, err := tx.ExecContext(r.Context(), `
			DELETE FROM channel_users
			WHERE user_id = $1
			  AND channel_id IN (
				SELECT c.id FROM channels c
				JOIN workspaces w ON w.id = c.workspace_id
				WHERE w.company_id = $2
			  )`, membership.UserID, companyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove channel access", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			DELETE FROM chat_users
			WHERE user_id = $1
			  AND chat_id IN (
				SELECT ch.id FROM chats ch
				JOIN workspaces w ON w.id = ch.workspace_id
				WHERE w.company_id = $2
			  )`, membership.UserID, companyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove chat access", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			DELETE FROM workspace_users
			WHERE user_id = $1
			  AND workspace_id IN (SELECT id FROM workspaces WHERE company_id = $2)`, membership.UserID, companyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove workspace access", http.StatusInternalServerError)
			return
		}
		if err := membership.Delete(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove company member", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not remove member", http.StatusInternalServerError)
			return
		}
		if membership.UserID == session.ID {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.Redirect(w, r, "/companies/"+companyID+"/members")
	}))
}
