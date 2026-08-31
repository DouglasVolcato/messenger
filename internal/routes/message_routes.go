package routes

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterMessageRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("POST /api/chats/{chatID}/messages", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		chatID := r.PathValue("chatID")
		var workspaceID, chatType string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `
            SELECT workspace_id, channel_id, type
            FROM chats
            WHERE id = $1`, chatID).Scan(&workspaceID, &channelID, &chatType); err != nil || workspaceID != user.WorkspaceID {
			http.NotFound(w, r)
			return
		}

		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			allowed = user.Role == "OWNER" || user.Role == "ADMIN"
			if !allowed {
				var channelType string
				if err := db.DB.QueryRowContext(r.Context(), `SELECT type FROM channels WHERE id = $1`, *channelID).Scan(&channelType); err == nil {
					allowed = channelType == "PUBLIC"
				}
			}
			if !allowed {
				var membershipID string
				allowed = db.DB.QueryRowContext(r.Context(), `
                    SELECT id FROM channel_users WHERE channel_id = $1 AND user_id = $2`, *channelID, user.ID).Scan(&membershipID) == nil
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `
                SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, chatID, user.ID).Scan(&membershipID) == nil
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		content := strings.TrimSpace(r.FormValue("content"))
		if content == "" {
			http.Error(w, "message cannot be empty", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		message := models.ChatMessage{
			ChatID:          chatID,
			UserID:          user.ID,
			ClientMessageID: r.FormValue("client_message_id"),
			Content:         content,
			Type:            "TEXT",
		}
		if err := message.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not send message", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not send message", http.StatusInternalServerError)
			return
		}

		if channelID != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+chatID)
	}))

	mux.Handle("POST /api/messages/{messageID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.UserID != user.ID {
			http.NotFound(w, r)
			return
		}
		var workspaceID string
		if err := db.DB.QueryRowContext(r.Context(), `SELECT workspace_id FROM chats WHERE id = $1`, message.ChatID).Scan(&workspaceID); err != nil || workspaceID != user.WorkspaceID {
			http.NotFound(w, r)
			return
		}
		message.Content = strings.TrimSpace(r.FormValue("content"))
		if message.Content == "" {
			http.Error(w, "message cannot be empty", http.StatusBadRequest)
			return
		}
		now := time.Now()
		message.EditedAt = &now

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := message.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not edit message", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not edit message", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID)
	}))

	mux.Handle("POST /api/messages/{messageID}/delete", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceID, chatType string
		if err := db.DB.QueryRowContext(r.Context(), `SELECT workspace_id, type FROM chats WHERE id = $1`, message.ChatID).Scan(&workspaceID, &chatType); err != nil || workspaceID != user.WorkspaceID {
			http.NotFound(w, r)
			return
		}
		if message.UserID != user.ID && !(chatType == "CHANNEL" && (user.Role == "OWNER" || user.Role == "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		now := time.Now()
		message.DeletedAt = &now

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		if err := message.Update(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not delete message", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not delete message", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID)
	}))

	mux.Handle("POST /api/messages/{messageID}/reactions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		var workspaceID, chatType string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `SELECT workspace_id, channel_id, type FROM chats WHERE id = $1`, message.ChatID).Scan(&workspaceID, &channelID, &chatType); err != nil || workspaceID != user.WorkspaceID {
			http.NotFound(w, r)
			return
		}
		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			allowed = user.Role == "OWNER" || user.Role == "ADMIN"
			if !allowed {
				var channelType string
				if err := db.DB.QueryRowContext(r.Context(), `SELECT type FROM channels WHERE id = $1`, *channelID).Scan(&channelType); err == nil {
					allowed = channelType == "PUBLIC"
				}
			}
			if !allowed {
				var membershipID string
				allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM channel_users WHERE channel_id = $1 AND user_id = $2`, *channelID, user.ID).Scan(&membershipID) == nil
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, message.ChatID, user.ID).Scan(&membershipID) == nil
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		reactionValue := strings.TrimSpace(r.FormValue("reaction"))
		if reactionValue == "" {
			http.Error(w, "reaction is required", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		reaction := models.ChatMessageReaction{ChatMessageID: message.ID, UserID: user.ID, Reaction: reactionValue}
		if err := reaction.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "reaction already exists", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add reaction", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID)
	}))
}
