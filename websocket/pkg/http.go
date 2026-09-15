package utils

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"
)

func SendErrorMessage(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set(
		"HX-Trigger",
		fmt.Sprintf(`{
        "errorMessage": {
            "message": %q
        }
    }`, message),
	)
	w.WriteHeader(statusCode)
}

func ExecuteTemplate(w http.ResponseWriter, templ *template.Template, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templ.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func Redirect(w http.ResponseWriter, r *http.Request, path string) {
	path = strings.ReplaceAll(path, "//", "/")
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Location", path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func SetUserCookie(w http.ResponseWriter, r *http.Request, u UserInput) error {
	token, err := GenerateJWT(u)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "user",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   os.Getenv("ENV") == "production",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
		MaxAge:   86400,
	})
	return nil
}

func ClearUserCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "user",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   os.Getenv("ENV") == "production",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

func GetUserFromCookie(r *http.Request) (*UserInput, error) {
	cookie, err := r.Cookie("user")
	if err != nil {
		return nil, err
	}
	return ValidateJWT(cookie.Value)
}
