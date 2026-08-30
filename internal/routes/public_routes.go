package routes

import (
	"html/template"
	"net/http"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterPublicRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		utils.ExecuteTemplate(w, templ, "index.html", &ViewData{AppVersion: appVersion})
	}))

	mux.Handle("GET /health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	mux.Handle("GET /logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		utils.ClearUserCookie(w, r)
		utils.Redirect(w, r, "/")
	}))

	fileServer := http.FileServer(http.Dir("static/"))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})
}
