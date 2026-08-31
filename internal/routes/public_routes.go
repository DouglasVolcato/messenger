package routes

import (
	"html/template"
	"net/http"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterPublicRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := utils.GetUserFromCookie(r); err == nil {
			utils.Redirect(w, r, "/workspaces")
			return
		}
		utils.Redirect(w, r, "/login")
	}))

	mux.Handle("GET /health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	fileServer := http.FileServer(http.Dir("static/"))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})
}
