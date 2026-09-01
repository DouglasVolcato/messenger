package routes

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterNotificationRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /notifications", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		backURL := "/workspaces"
		workspaceContextID := r.URL.Query().Get("workspace")
		if workspaceContextID != "" {
			var activeWorkspaceID string
			if db.DB.QueryRowContext(r.Context(), `
				SELECT workspace_id FROM workspace_users
				WHERE workspace_id = $1 AND user_id = $2 AND status = 'ACTIVE'`, workspaceContextID, session.ID).Scan(&activeWorkspaceID) == nil {
				workspaceContextID = activeWorkspaceID
				backURL = "/workspaces/" + activeWorkspaceID
			} else {
				workspaceContextID = ""
			}
		}

		notification := models.UserNotification{UserID: user.ID}
		items, total, err := notification.GetMany(db.DB, r.Context(), page, 25)
		if err != nil {
			http.Error(w, "could not load notifications", http.StatusInternalServerError)
			return
		}
		utils.ExecuteTemplate(w, templ, "notifications/index.html", &ViewData{
			AppVersion:         appVersion,
			Notifications:      items,
			BackURL:            backURL,
			WorkspaceContextID: workspaceContextID,
			Page:               page,
			PrevPage:           page - 1,
			NextPage:           page + 1,
			Limit:              25,
			Total:              total,
			HasPrev:            page > 1,
			HasNext:            int64(page*25) < total,
			Success:            r.URL.Query().Get("success"),
		})
	}))

	mux.Handle("GET /notifications/{notificationID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		var actionURL *string
		err = db.DB.QueryRowContext(r.Context(), `
			UPDATE user_notifications
			SET is_read = TRUE, read_at = COALESCE(read_at, NOW())
			WHERE id = $1 AND user_id = $2
			RETURNING action_url`, r.PathValue("notificationID"), user.ID).Scan(&actionURL)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if actionURL != nil && strings.HasPrefix(*actionURL, "/") && !strings.HasPrefix(*actionURL, "//") {
			utils.Redirect(w, r, *actionURL)
			return
		}
		utils.Redirect(w, r, "/notifications")
	}))

	mux.Handle("POST /api/notifications/read-all", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if _, err := db.DB.ExecContext(r.Context(), `
			UPDATE user_notifications
			SET is_read = TRUE, read_at = COALESCE(read_at, NOW())
			WHERE user_id = $1 AND is_read = FALSE`, user.ID); err != nil {
			http.Error(w, "could not update notifications", http.StatusInternalServerError)
			return
		}
		redirectURL := "/notifications?success=Notifications+marked+as+read"
		if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
			redirectURL += "&workspace=" + workspaceID
		}
		utils.Redirect(w, r, redirectURL)
	}))

	mux.Handle("POST /api/notifications/{notificationID}/read", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		result, err := db.DB.ExecContext(r.Context(), `
			UPDATE user_notifications
			SET is_read = TRUE, read_at = COALESCE(read_at, NOW())
			WHERE id = $1 AND user_id = $2`, r.PathValue("notificationID"), user.ID)
		if err != nil {
			http.Error(w, "could not update notification", http.StatusInternalServerError)
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			http.NotFound(w, r)
			return
		}
		redirectURL := "/notifications"
		if workspaceID := r.FormValue("workspace_id"); workspaceID != "" {
			redirectURL += "?workspace=" + workspaceID
		}
		utils.Redirect(w, r, redirectURL)
	}))
}
