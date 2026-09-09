package routes

import (
	"html/template"
	"net/http"
	"testing"
)

func TestRegisterRoutesDoesNotPanic(t *testing.T) {
	mux := http.NewServeMux()
	templ := template.New("test")

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("route registration panicked: %v", recovered)
		}
	}()

	RegisterRoutes(mux, templ, "test")
}
