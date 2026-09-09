package routes

import (
	"html/template"
	"net/http"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterPublicRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := utils.GetUserFromCookie(r); err == nil {
			utils.Redirect(w, r, "/companies")
			return
		}
		utils.ExecuteTemplate(w, templ, "index.html", &ViewData{AppVersion: appVersion})
	}))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
}
