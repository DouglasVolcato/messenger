package routes

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterSettingsRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /settings/profile", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		user := models.User{ID: session.ID}
		if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}
		backURL := "/workspaces"
		workspaceContextID := r.URL.Query().Get("workspace")
		if workspaceContextID != "" {
			var activeWorkspaceID string
			if db.DB.QueryRowContext(r.Context(), `
				SELECT wu.workspace_id
				FROM workspace_users wu
				JOIN workspaces w ON w.id = wu.workspace_id
				JOIN companies c ON c.id = w.company_id
				WHERE wu.workspace_id = $1 AND wu.user_id = $2
				  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE'`, workspaceContextID, session.ID).Scan(&activeWorkspaceID) == nil {
				workspaceContextID = activeWorkspaceID
				backURL = "/workspaces/" + activeWorkspaceID
			} else {
				workspaceContextID = ""
			}
		}
		utils.ExecuteTemplate(w, templ, "settings/profile.html", &ViewData{
			AppVersion:         appVersion,
			User:               user,
			BackURL:            backURL,
			WorkspaceContextID: workspaceContextID,
			Success:            r.URL.Query().Get("success"),
			Error:              r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/settings/profile", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		user := models.User{ID: session.ID}
		if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}
		user.Name = strings.TrimSpace(r.FormValue("name"))
		user.Username = strings.TrimSpace(r.FormValue("username"))
		user.Email = strings.TrimSpace(r.FormValue("email"))
		if user.Name == "" || user.Username == "" || user.Email == "" {
			redirectURL := "/settings/profile?error=Name,+username+and+email+are+required"
			if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
				redirectURL += "&workspace=" + workspaceID
			}
			utils.Redirect(w, r, redirectURL)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := user.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			redirectURL := "/settings/profile?error=Username+or+email+already+in+use"
			if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
				redirectURL += "&workspace=" + workspaceID
			}
			utils.Redirect(w, r, redirectURL)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update profile", http.StatusInternalServerError)
			return
		}
		redirectURL := "/settings/profile?success=Profile+updated"
		if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
			redirectURL += "&workspace=" + workspaceID
		}
		utils.Redirect(w, r, redirectURL)
	}))

	mux.Handle("GET /settings/security", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		user := models.User{ID: session.ID}
		if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}
		backURL := "/settings/profile"
		workspaceContextID := r.URL.Query().Get("workspace")
		if workspaceContextID != "" {
			var activeWorkspaceID string
			if db.DB.QueryRowContext(r.Context(), `SELECT workspace_id FROM workspace_users WHERE workspace_id = $1 AND user_id = $2 AND status = 'ACTIVE'`, workspaceContextID, session.ID).Scan(&activeWorkspaceID) == nil {
				workspaceContextID = activeWorkspaceID
				backURL += "?workspace=" + activeWorkspaceID
			} else {
				workspaceContextID = ""
			}
		}
		utils.ExecuteTemplate(w, templ, "settings/security.html", &ViewData{
			AppVersion:         appVersion,
			User:               user,
			BackURL:            backURL,
			WorkspaceContextID: workspaceContextID,
			Success:            r.URL.Query().Get("success"),
			Error:              r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/settings/security", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if r.FormValue("new_password") == "" || r.FormValue("new_password") != r.FormValue("confirm_password") {
			redirectURL := "/settings/security?error=New+passwords+do+not+match"
			if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
				redirectURL += "&workspace=" + workspaceID
			}
			utils.Redirect(w, r, redirectURL)
			return
		}
		user := models.User{ID: session.ID}
		if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}
		if !utils.ComparePassword(user.PasswordHash, r.FormValue("current_password")) {
			redirectURL := "/settings/security?error=Current+password+is+invalid"
			if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
				redirectURL += "&workspace=" + workspaceID
			}
			utils.Redirect(w, r, redirectURL)
			return
		}
		user.Password = r.FormValue("new_password")
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := user.UpdatePassword(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not update password", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update password", http.StatusInternalServerError)
			return
		}
		redirectURL := "/settings/security?success=Password+updated"
		if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
			redirectURL += "&workspace=" + workspaceID
		}
		utils.Redirect(w, r, redirectURL)
	}))
}
