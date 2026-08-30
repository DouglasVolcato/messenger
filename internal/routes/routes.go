package routes

import (
	"html/template"
	"net/http"
	"time"
)

const DEFAULT_TIMEOUT = 5 * time.Second

type ViewData struct {
	AppVersion string
}

func RegisterRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	RegisterPublicRoutes(mux, templ, appVersion)
}
