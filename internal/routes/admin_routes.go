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

func loadSystemAdmin(r *http.Request) (models.User, bool) {
	session, err := utils.GetUserFromCookie(r)
	if err != nil {
		return models.User{}, false
	}
	user := models.User{ID: session.ID}
	if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" || !user.SystemAdmin {
		return models.User{}, false
	}
	return user, true
}

func RegisterAdminRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /admin/login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); ok {
			utils.Redirect(w, r, "/admin/companies")
			return
		}
		utils.ExecuteTemplate(w, templ, "admin/login.html", &ViewData{AppVersion: appVersion})
	}))

	mux.Handle("POST /api/admin/login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		identifier := strings.TrimSpace(r.FormValue("identifier"))
		user := models.User{Email: identifier}
		err := user.GetOneByEmail(db.DB, r.Context())
		if err == sql.ErrNoRows {
			user = models.User{Username: identifier}
			err = user.GetOneByUsername(db.DB, r.Context())
		}
		if err != nil || user.Status != "ACTIVE" || !user.SystemAdmin || !utils.ComparePassword(user.PasswordHash, r.FormValue("password")) {
			utils.ExecuteTemplate(w, templ, "admin/login.html", &ViewData{
				AppVersion: appVersion,
				Error:      "Invalid administrator credentials.",
				Identifier: identifier,
			})
			return
		}
		if err := utils.SetUserCookie(w, r, utils.UserInput{ID: user.ID, SystemAdmin: true}); err != nil {
			http.Error(w, "could not create session", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/companies")
	}))

	mux.Handle("GET /admin/logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		utils.ClearUserCookie(w, r)
		utils.Redirect(w, r, "/admin/login")
	}))

	mux.Handle("GET /admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		utils.Redirect(w, r, "/admin/companies")
	}))

	mux.Handle("GET /admin/companies", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT c.id, c.name, c.status, c.created_at, COUNT(cu.id), COUNT(*) OVER()
			FROM companies c
			LEFT JOIN company_users cu ON cu.company_id = c.id
			WHERE ($1 = '' OR c.name ILIKE '%' || $1 || '%')
			GROUP BY c.id, c.name, c.status, c.created_at
			ORDER BY c.created_at DESC, c.id DESC
			LIMIT $2 OFFSET $3`, query, limit, (page-1)*limit)
		if err != nil {
			http.Error(w, "could not load companies", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]AdminCompanyView, 0)
		var total int64
		for rows.Next() {
			var item AdminCompanyView
			if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt, &item.UserCount, &total); err != nil {
				http.Error(w, "could not load companies", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		utils.ExecuteTemplate(w, templ, "admin/companies/index.html", &ViewData{
			AppVersion:     appVersion,
			User:           admin,
			AdminCompanies: items,
			Query:          query,
			Page:           page,
			PrevPage:       page - 1,
			NextPage:       page + 1,
			Total:          total,
			HasPrev:        page > 1,
			HasNext:        int64(page*limit) < total,
		})
	}))

	mux.Handle("GET /admin/companies/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		utils.ExecuteTemplate(w, templ, "admin/companies/new.html", &ViewData{AppVersion: appVersion, User: admin})
	}))

	mux.Handle("POST /api/admin/companies", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		company := models.Company{Name: strings.TrimSpace(r.FormValue("name")), Status: strings.ToUpper(r.FormValue("status"))}
		if company.Name == "" {
			http.Error(w, "company name is required", http.StatusBadRequest)
			return
		}
		if company.Status == "" {
			company.Status = "ACTIVE"
		}
		if company.Status != "ACTIVE" && company.Status != "SUSPENDED" && company.Status != "DISABLED" {
			http.Error(w, "invalid company status", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := company.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create company", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create company", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/companies/"+company.ID+"/edit")
	}))

	mux.Handle("GET /admin/companies/{companyID}/edit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		company := models.Company{ID: r.PathValue("companyID")}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		utils.ExecuteTemplate(w, templ, "admin/companies/edit.html", &ViewData{AppVersion: appVersion, User: admin, Company: company})
	}))

	mux.Handle("POST /api/admin/companies/{companyID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		company := models.Company{ID: r.PathValue("companyID")}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		company.Name = strings.TrimSpace(r.FormValue("name"))
		company.Status = strings.ToUpper(r.FormValue("status"))
		if company.Name == "" || (company.Status != "ACTIVE" && company.Status != "SUSPENDED" && company.Status != "DISABLED") {
			http.Error(w, "invalid company data", http.StatusBadRequest)
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
		utils.Redirect(w, r, "/admin/companies/"+company.ID+"/edit")
	}))

	mux.Handle("GET /admin/companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		company := models.Company{ID: r.PathValue("companyID")}
		if err := company.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT cu.id, u.id, u.name, u.username, u.email, cu.role, u.status, COUNT(*) OVER()
			FROM company_users cu
			JOIN users u ON u.id = cu.user_id
			WHERE cu.company_id = $1
			ORDER BY cu.created_at ASC, u.name ASC
			LIMIT $2 OFFSET $3`, company.ID, limit, (page-1)*limit)
		if err != nil {
			http.Error(w, "could not load company members", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		members := make([]MemberView, 0)
		var total int64
		for rows.Next() {
			var member MemberView
			if err := rows.Scan(&member.ID, &member.UserID, &member.Name, &member.Username, &member.Email, &member.Role, &member.Status, &total); err != nil {
				http.Error(w, "could not load company members", http.StatusInternalServerError)
				return
			}
			members = append(members, member)
		}
		utils.ExecuteTemplate(w, templ, "admin/companies/members.html", &ViewData{
			AppVersion: appVersion,
			User:       admin,
			Company:    company,
			Members:    members,
			Page:       page,
			PrevPage:   page - 1,
			NextPage:   page + 1,
			Total:      total,
			HasPrev:    page > 1,
			HasNext:    int64(page*limit) < total,
		})
	}))

	mux.Handle("POST /api/admin/companies/{companyID}/members", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
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
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" && role != "GUEST" {
			http.Error(w, "invalid company role", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		membership := models.CompanyUser{CompanyID: r.PathValue("companyID"), UserID: target.ID, Role: role}
		if err := membership.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "user already belongs to this company", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add company member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/companies/"+r.PathValue("companyID")+"/members")
	}))

	mux.Handle("POST /api/admin/companies/{companyID}/members/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		role := strings.ToUpper(r.FormValue("role"))
		if role != "OWNER" && role != "ADMIN" && role != "MEMBER" && role != "GUEST" {
			http.Error(w, "invalid company role", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		var companyID string
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM companies WHERE id = $1 FOR UPDATE`, r.PathValue("companyID")).Scan(&companyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: r.PathValue("userID")}
		if err := tx.QueryRowContext(r.Context(), `SELECT id, role FROM company_users WHERE company_id = $1 AND user_id = $2`, companyID, membership.UserID).Scan(&membership.ID, &membership.Role); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		if membership.Role == "OWNER" && role != "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM company_users WHERE company_id = $1 AND role = 'OWNER'`, companyID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last company owner cannot be demoted", http.StatusBadRequest)
				return
			}
		}
		membership.Role = role
		if err := membership.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update company role", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update company role", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/companies/"+companyID+"/members")
	}))

	mux.Handle("POST /api/admin/companies/{companyID}/members/{userID}/remove", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		var companyID string
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM companies WHERE id = $1 FOR UPDATE`, r.PathValue("companyID")).Scan(&companyID); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		membership := models.CompanyUser{CompanyID: companyID, UserID: r.PathValue("userID")}
		if err := tx.QueryRowContext(r.Context(), `SELECT id, role FROM company_users WHERE company_id = $1 AND user_id = $2`, companyID, membership.UserID).Scan(&membership.ID, &membership.Role); err != nil {
			_ = db.RollbackTransaction(tx)
			http.NotFound(w, r)
			return
		}
		if membership.Role == "OWNER" {
			var owners int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM company_users WHERE company_id = $1 AND role = 'OWNER'`, companyID).Scan(&owners); err != nil || owners <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last company owner cannot be removed", http.StatusBadRequest)
				return
			}
		}
		if err := membership.Delete(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not remove company member", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not remove company member", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/companies/"+companyID+"/members")
	}))

	mux.Handle("GET /admin/users", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
		if status != "" && status != "ACTIVE" && status != "BLOCKED" && status != "DISABLED" {
			status = ""
		}
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT u.id, u.name, u.username, u.email, u.status, u.is_system_admin, u.created_at,
			       COUNT(cu.id), COUNT(*) OVER()
			FROM users u
			LEFT JOIN company_users cu ON cu.user_id = u.id
			WHERE ($1 = '' OR u.name ILIKE '%' || $1 || '%' OR u.username ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%')
			  AND ($2 = '' OR u.status = $2)
			GROUP BY u.id, u.name, u.username, u.email, u.status, u.is_system_admin, u.created_at
			ORDER BY u.created_at DESC, u.id DESC
			LIMIT $3 OFFSET $4`, query, status, limit, (page-1)*limit)
		if err != nil {
			http.Error(w, "could not load users", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := make([]AdminUserView, 0)
		var total int64
		for rows.Next() {
			var item AdminUserView
			if err := rows.Scan(&item.ID, &item.Name, &item.Username, &item.Email, &item.Status, &item.SystemAdmin, &item.CreatedAt, &item.CompanyCount, &total); err != nil {
				http.Error(w, "could not load users", http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		utils.ExecuteTemplate(w, templ, "admin/users/index.html", &ViewData{
			AppVersion:  appVersion,
			User:        admin,
			AdminUsers:  items,
			Query:       query,
			StatusFilter: status,
			Page:        page,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			Total:       total,
			HasPrev:     page > 1,
			HasNext:     int64(page*limit) < total,
		})
	}))

	mux.Handle("GET /admin/users/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		utils.ExecuteTemplate(w, templ, "admin/users/new.html", &ViewData{AppVersion: appVersion, User: admin})
	}))

	mux.Handle("POST /api/admin/users", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := loadSystemAdmin(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		user := models.User{
			Name:        strings.TrimSpace(r.FormValue("name")),
			Username:    strings.TrimSpace(r.FormValue("username")),
			Email:       strings.TrimSpace(r.FormValue("email")),
			Password:    r.FormValue("password"),
			Status:      strings.ToUpper(r.FormValue("status")),
			SystemAdmin: r.FormValue("system_admin") == "on",
		}
		if user.Status == "" {
			user.Status = "ACTIVE"
		}
		if user.Name == "" || user.Username == "" || user.Email == "" || user.Password == "" || (user.Status != "ACTIVE" && user.Status != "BLOCKED" && user.Status != "DISABLED") {
			http.Error(w, "invalid user data", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := user.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "username or email already in use", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create user", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/admin/users/"+user.ID+"/edit")
	}))

	mux.Handle("GET /admin/users/{userID}/edit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			utils.Redirect(w, r, "/admin/login")
			return
		}
		target := models.User{ID: r.PathValue("userID")}
		if err := target.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		rows, err := db.DB.QueryContext(r.Context(), `
			SELECT c.id, c.name, cu.role
			FROM company_users cu
			JOIN companies c ON c.id = cu.company_id
			WHERE cu.user_id = $1
			ORDER BY c.name`, target.ID)
		if err != nil {
			http.Error(w, "could not load memberships", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		companies := make([]AdminUserCompanyView, 0)
		for rows.Next() {
			var item AdminUserCompanyView
			if err := rows.Scan(&item.CompanyID, &item.CompanyName, &item.Role); err != nil {
				http.Error(w, "could not load memberships", http.StatusInternalServerError)
				return
			}
			companies = append(companies, item)
		}
		utils.ExecuteTemplate(w, templ, "admin/users/edit.html", &ViewData{AppVersion: appVersion, User: target, Member: MemberView{UserID: admin.ID}, UserCompanies: companies})
	}))

	mux.Handle("POST /api/admin/users/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, ok := loadSystemAdmin(r)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		target := models.User{ID: r.PathValue("userID")}
		if err := target.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		target.Name = strings.TrimSpace(r.FormValue("name"))
		target.Username = strings.TrimSpace(r.FormValue("username"))
		target.Email = strings.TrimSpace(r.FormValue("email"))
		newStatus := strings.ToUpper(r.FormValue("status"))
		newSystemAdmin := r.FormValue("system_admin") == "on"
		if target.Name == "" || target.Username == "" || target.Email == "" || (newStatus != "ACTIVE" && newStatus != "BLOCKED" && newStatus != "DISABLED") {
			http.Error(w, "invalid user data", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not lock users", http.StatusInternalServerError)
			return
		}
		if target.SystemAdmin && target.Status == "ACTIVE" && (!newSystemAdmin || newStatus != "ACTIVE") {
			var admins int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_system_admin = TRUE AND status = 'ACTIVE'`).Scan(&admins); err != nil || admins <= 1 {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "the last active system administrator cannot be disabled or demoted", http.StatusBadRequest)
				return
			}
		}
		target.Status = newStatus
		target.SystemAdmin = newSystemAdmin
		if err := target.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "username or email already in use", http.StatusBadRequest)
			return
		}
		if r.FormValue("password") != "" {
			target.Password = r.FormValue("password")
			if err := target.UpdatePassword(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not update password", http.StatusInternalServerError)
				return
			}
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update user", http.StatusInternalServerError)
			return
		}
		if target.ID == admin.ID && (!target.SystemAdmin || target.Status != "ACTIVE") {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/admin/login")
			return
		}
		utils.Redirect(w, r, "/admin/users/"+target.ID+"/edit")
	}))
}
