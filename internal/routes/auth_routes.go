package routes

import (
	"database/sql"
	"html/template"
	"net/http"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterAuthRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := utils.GetUserFromCookie(r); err == nil {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.ExecuteTemplate(w, templ, "auth/login.html", &ViewData{AppVersion: appVersion})
	}))

	mux.Handle("POST /api/auth/login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if err != nil || user.Status != "ACTIVE" || !utils.ComparePassword(user.PasswordHash, r.FormValue("password")) {
			utils.ExecuteTemplate(w, templ, "auth/login.html", &ViewData{
				AppVersion: appVersion,
				Error:      "Invalid email, username or password.",
				Identifier: identifier,
			})
			return
		}

		if err := utils.SetUserCookie(w, r, utils.UserInput{ID: user.ID}); err != nil {
			http.Error(w, "could not create session", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces")
	}))

	mux.Handle("GET /register", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := utils.GetUserFromCookie(r); err == nil {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.ExecuteTemplate(w, templ, "auth/register.html", &ViewData{AppVersion: appVersion})
	}))

	mux.Handle("POST /api/auth/register", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		data := &ViewData{
			AppVersion: appVersion,
			Name:       strings.TrimSpace(r.FormValue("name")),
			Username:   strings.TrimSpace(r.FormValue("username")),
			Email:      strings.TrimSpace(r.FormValue("email")),
		}
		if data.Name == "" || data.Username == "" || data.Email == "" || r.FormValue("password") == "" {
			data.Error = "Fill in all required fields."
			utils.ExecuteTemplate(w, templ, "auth/register.html", data)
			return
		}
		if r.FormValue("password") != r.FormValue("confirm_password") {
			data.Error = "Passwords do not match."
			utils.ExecuteTemplate(w, templ, "auth/register.html", data)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		user := models.User{
			Name:     data.Name,
			Username: data.Username,
			Email:    data.Email,
			Password: r.FormValue("password"),
		}
		if err := user.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			data.Error = "Username or email already in use."
			utils.ExecuteTemplate(w, templ, "auth/register.html", data)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create account", http.StatusInternalServerError)
			return
		}

		if err := utils.SetUserCookie(w, r, utils.UserInput{ID: user.ID}); err != nil {
			http.Error(w, "could not create session", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces")
	}))

	logout := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		utils.ClearUserCookie(w, r)
		utils.Redirect(w, r, "/login")
	})
	mux.Handle("GET /logout", logout)
	mux.Handle("POST /logout", logout)
}
