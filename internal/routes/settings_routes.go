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
		if err := user.GetOne(db.DB, r.Context()); err != nil {
			utils.ClearUserCookie(w, r)
			utils.Redirect(w, r, "/login")
			return
		}
		utils.ExecuteTemplate(w, templ, "settings/profile.html", &ViewData{
			AppVersion: appVersion,
			User:       user,
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
		if err := user.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		user.Name = strings.TrimSpace(r.FormValue("name"))
		user.Username = strings.TrimSpace(r.FormValue("username"))
		user.Email = strings.TrimSpace(r.FormValue("email"))
		if user.Name == "" || user.Username == "" || user.Email == "" {
			http.Error(w, "name, username and email are required", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := user.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "username or email already in use", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update profile", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/settings/profile")
	}))

	mux.Handle("GET /settings/security", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := utils.GetUserFromCookie(r); err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		utils.ExecuteTemplate(w, templ, "settings/security.html", &ViewData{AppVersion: appVersion})
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
			http.Error(w, "new passwords do not match", http.StatusBadRequest)
			return
		}

		user := models.User{ID: session.ID}
		if err := user.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		if !utils.ComparePassword(user.PasswordHash, r.FormValue("current_password")) {
			http.Error(w, "current password is invalid", http.StatusBadRequest)
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
		utils.Redirect(w, r, "/settings/security")
	}))
}
