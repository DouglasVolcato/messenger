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
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		chatID := r.PathValue("chatID")
		var workspaceID, chatType, workspaceRole string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT ch.workspace_id, ch.channel_id, ch.type, wu.role
			FROM chats ch
			JOIN workspaces w ON w.id = ch.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = ch.workspace_id AND wu.user_id = $2
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = $2
			JOIN users u ON u.id = $2
			WHERE ch.id = $1
			  AND wu.status = 'ACTIVE'
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, chatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `
				SELECT c.type, COALESCE(cu.role, '')
				FROM channels c
				LEFT JOIN channel_users cu ON cu.channel_id = c.id AND cu.user_id = $2
				WHERE c.id = $1 AND c.workspace_id = $3`, *channelID, session.ID, workspaceID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `
				SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, chatID, session.ID).Scan(&membershipID) == nil
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
			UserID:          session.ID,
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
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.UserID != session.ID || message.DeletedAt != nil {
			http.NotFound(w, r)
			return
		}

		var workspaceID, chatType, workspaceRole string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT ch.workspace_id, ch.channel_id, ch.type, wu.role
			FROM chats ch
			JOIN workspaces w ON w.id = ch.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = ch.workspace_id AND wu.user_id = $2
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = $2
			JOIN users u ON u.id = $2
			WHERE ch.id = $1
			  AND wu.status = 'ACTIVE'
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `
				SELECT c.type, COALESCE(cu.role, '')
				FROM channels c
				LEFT JOIN channel_users cu ON cu.channel_id = c.id AND cu.user_id = $2
				WHERE c.id = $1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&membershipID) == nil
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
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
		if channelID != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID)
	}))

	mux.Handle("POST /api/messages/{messageID}/delete", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}

		var workspaceID, chatType, workspaceRole string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT ch.workspace_id, ch.channel_id, ch.type, wu.role
			FROM chats ch
			JOIN workspaces w ON w.id = ch.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = ch.workspace_id AND wu.user_id = $2
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = $2
			JOIN users u ON u.id = $2
			WHERE ch.id = $1
			  AND wu.status = 'ACTIVE'
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed := false
		channelRole := ""
		if chatType == "CHANNEL" && channelID != nil {
			var channelType string
			if err := db.DB.QueryRowContext(r.Context(), `
				SELECT c.type, COALESCE(cu.role, '')
				FROM channels c
				LEFT JOIN channel_users cu ON cu.channel_id = c.id AND cu.user_id = $2
				WHERE c.id = $1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&membershipID) == nil
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if message.UserID != session.ID && !(chatType == "CHANNEL" && (workspaceRole == "OWNER" || workspaceRole == "ADMIN" || channelRole == "OWNER" || channelRole == "ADMIN")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if message.DeletedAt == nil {
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
		}
		if channelID != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID)
	}))

	mux.Handle("POST /api/messages/{messageID}/reactions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		message := models.ChatMessage{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.DeletedAt != nil {
			http.NotFound(w, r)
			return
		}

		var workspaceID, chatType, workspaceRole string
		var channelID *string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT ch.workspace_id, ch.channel_id, ch.type, wu.role
			FROM chats ch
			JOIN workspaces w ON w.id = ch.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = ch.workspace_id AND wu.user_id = $2
			JOIN company_users company_member ON company_member.company_id = w.company_id AND company_member.user_id = $2
			JOIN users u ON u.id = $2
			WHERE ch.id = $1
			  AND wu.status = 'ACTIVE'
			  AND w.status = 'ACTIVE'
			  AND c.status = 'ACTIVE'
			  AND u.status = 'ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `
				SELECT c.type, COALESCE(cu.role, '')
				FROM channels c
				LEFT JOIN channel_users cu ON cu.channel_id = c.id AND cu.user_id = $2
				WHERE c.id = $1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&membershipID) == nil
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
		reaction := models.ChatMessageReaction{ChatMessageID: message.ID, UserID: session.ID, Reaction: reactionValue}
		if err := reaction.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "reaction already exists", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not add reaction", http.StatusInternalServerError)
			return
		}
		if channelID != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID)
	}))
}
