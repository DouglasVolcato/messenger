package routes

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterSettingsRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /settings/profile", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r); if !ok { return }
		utils.ExecuteTemplate(w, templ, "settings/profile.html", &ViewData{AppVersion: appVersion, User: user, BackURL: "/companies", Success: r.URL.Query().Get("success"), Error: r.URL.Query().Get("error")})
	}))

	mux.Handle("POST /api/settings/profile", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r); if !ok { return }
		if err := r.ParseForm(); err != nil { http.Error(w, "invalid form", http.StatusBadRequest); return }
		user.Name = strings.TrimSpace(r.FormValue("name")); user.Username = strings.TrimSpace(r.FormValue("username")); user.Email = strings.TrimSpace(r.FormValue("email"))
		if user.Name == "" || user.Username == "" || user.Email == "" { utils.Redirect(w, r, "/settings/profile?error=Name,+username+and+email+are+required"); return }
		tx, err := db.BeginTransaction(r.Context()); if err != nil { http.Error(w, "could not start transaction", http.StatusInternalServerError); return }
		if err := user.Update(tx, r.Context()); err != nil { _ = db.RollbackTransaction(tx); utils.Redirect(w, r, "/settings/profile?error=Username+or+email+already+in+use"); return }
		if err := db.CommitTransaction(tx); err != nil { http.Error(w, "could not update profile", http.StatusInternalServerError); return }
		_ = cache.DeleteUserCache(r.Context(), user.ID)
		invalidateUserCompanyCaches(r.Context(), user.ID)
		utils.Redirect(w, r, "/settings/profile?success=Profile+updated")
	}))

	mux.Handle("GET /settings/security", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r); if !ok { return }
		utils.ExecuteTemplate(w, templ, "settings/security.html", &ViewData{AppVersion: appVersion, User: user, BackURL: "/settings/profile", Success: r.URL.Query().Get("success"), Error: r.URL.Query().Get("error")})
	}))

	mux.Handle("POST /api/settings/security", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r); if !ok { return }
		if err := r.ParseForm(); err != nil { http.Error(w, "invalid form", http.StatusBadRequest); return }
		if r.FormValue("new_password") == "" || r.FormValue("new_password") != r.FormValue("confirm_password") { utils.Redirect(w, r, "/settings/security?error=New+passwords+do+not+match"); return }
		if !utils.ComparePassword(user.PasswordHash, r.FormValue("current_password")) { utils.Redirect(w, r, "/settings/security?error=Current+password+is+invalid"); return }
		user.Password = r.FormValue("new_password")
		tx, err := db.BeginTransaction(r.Context()); if err != nil { http.Error(w, "could not start transaction", http.StatusInternalServerError); return }
		if err := user.UpdatePassword(tx, r.Context()); err != nil { _ = db.RollbackTransaction(tx); http.Error(w, "could not update password", http.StatusInternalServerError); return }
		if err := db.CommitTransaction(tx); err != nil { http.Error(w, "could not update password", http.StatusInternalServerError); return }
		_ = cache.DeleteUserCache(r.Context(), user.ID)
		utils.Redirect(w, r, "/settings/security?success=Password+updated")
	}))
}
