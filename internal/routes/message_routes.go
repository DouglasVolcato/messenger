package routes

import (
	"database/sql"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func RegisterMessageRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /workspaces/{workspaceID}/messages/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		workspaceID := r.PathValue("workspaceID")
		var workspace models.Workspace
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT w.id, w.company_id, w.name, w.slug, w.status, w.created_at, w.updated_at
			FROM workspaces w
			JOIN companies c ON c.id = w.company_id
			JOIN workspace_users wu ON wu.workspace_id = w.id AND wu.user_id = $2
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = $2
			JOIN users u ON u.id = $2
			WHERE w.id = $1 AND w.status = 'ACTIVE' AND c.status = 'ACTIVE'
			  AND wu.status = 'ACTIVE' AND u.status = 'ACTIVE'`, workspaceID, session.ID).Scan(
			&workspace.ID, &workspace.CompanyID, &workspace.Name, &workspace.Slug, &workspace.Status, &workspace.CreatedAt, &workspace.UpdatedAt,
		); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		utils.ExecuteTemplate(w, templ, "messages/new.html", &ViewData{
			AppVersion: appVersion,
			Workspace:  workspace,
			BackURL:    "/workspaces/" + workspaceID,
			Error:      r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/workspaces/{workspaceID}/direct-messages", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := utils.GetUserFromCookie(r)
		if err != nil {
			utils.Redirect(w, r, "/login")
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		workspaceID := r.PathValue("workspaceID")
		var actorName string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT u.name
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN companies c ON c.id = w.company_id
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
			JOIN users u ON u.id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2
			  AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, workspaceID, session.ID).Scan(&actorName); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		identifier := strings.TrimSpace(r.FormValue("identifier"))
		target := models.User{Email: identifier}
		err = target.GetOneByEmail(db.DB, r.Context())
		if err == sql.ErrNoRows {
			target = models.User{Username: identifier}
			err = target.GetOneByUsername(db.DB, r.Context())
		}
		if err != nil || target.Status != "ACTIVE" {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"/messages/new?error=User+not+found")
			return
		}
		if target.ID == session.ID {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"/messages/new?error=Choose+another+user")
			return
		}
		var targetMembership string
		if err := db.DB.QueryRowContext(r.Context(), `
			SELECT wu.id
			FROM workspace_users wu
			JOIN workspaces w ON w.id = wu.workspace_id
			JOIN company_users cu ON cu.company_id = w.company_id AND cu.user_id = wu.user_id
			WHERE wu.workspace_id = $1 AND wu.user_id = $2 AND wu.status = 'ACTIVE'`, workspaceID, target.ID).Scan(&targetMembership); err != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"/messages/new?error=User+is+not+active+in+this+workspace")
			return
		}

		var existingChatID string
		err = db.DB.QueryRowContext(r.Context(), `
			SELECT ch.id
			FROM chats ch
			JOIN chat_users mine ON mine.chat_id = ch.id AND mine.user_id = $2 AND mine.left_at IS NULL
			JOIN chat_users other ON other.chat_id = ch.id AND other.user_id = $3 AND other.left_at IS NULL
			WHERE ch.workspace_id = $1 AND ch.type = 'DIRECT'
			  AND (SELECT COUNT(*) FROM chat_users cu WHERE cu.chat_id = ch.id AND cu.left_at IS NULL) = 2
			LIMIT 1`, workspaceID, session.ID, target.ID).Scan(&existingChatID)
		if err == nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+existingChatID)
			return
		}
		if err != sql.ErrNoRows {
			http.Error(w, "could not check conversation", http.StatusInternalServerError)
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		chat := models.Chat{WorkspaceID: workspaceID, Type: "DIRECT"}
		if err := chat.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create conversation", http.StatusInternalServerError)
			return
		}
		mine := models.ChatUser{ChatID: chat.ID, UserID: session.ID}
		if err := mine.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create conversation membership", http.StatusInternalServerError)
			return
		}
		other := models.ChatUser{ChatID: chat.ID, UserID: target.ID}
		if err := other.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create conversation membership", http.StatusInternalServerError)
			return
		}
		title := "New conversation"
		actionURL := "/workspaces/" + workspaceID + "?chat=" + chat.ID
		notification := models.UserNotification{UserID: target.ID, Type: "DIRECT_CHAT", Title: &title, Content: actorName + " started a conversation with you.", ActionURL: &actionURL}
		if err := notification.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not create notification", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not create conversation", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+chat.ID)
	}))

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
			WHERE ch.id = $1 AND wu.status = 'ACTIVE' AND w.status = 'ACTIVE' AND c.status = 'ACTIVE' AND u.status = 'ACTIVE'`, chatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `SELECT c.type, COALESCE(cu.role, '') FROM channels c LEFT JOIN channel_users cu ON cu.channel_id = c.id AND cu.user_id = $2 WHERE c.id = $1 AND c.workspace_id = $3`, *channelID, session.ID, workspaceID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var membershipID string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`, chatID, session.ID).Scan(&membershipID) == nil
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
		message := models.ChatMessage{ChatID: chatID, UserID: session.ID, ClientMessageID: r.FormValue("client_message_id"), Content: content, Type: "TEXT"}
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
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID+"#message-"+message.ID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+chatID+"#message-"+message.ID)
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
		if err := db.DB.QueryRowContext(r.Context(), `SELECT ch.workspace_id,ch.channel_id,ch.type,wu.role FROM chats ch JOIN workspaces w ON w.id=ch.workspace_id JOIN companies c ON c.id=w.company_id JOIN workspace_users wu ON wu.workspace_id=ch.workspace_id AND wu.user_id=$2 JOIN company_users company_member ON company_member.company_id=w.company_id AND company_member.user_id=$2 JOIN users u ON u.id=$2 WHERE ch.id=$1 AND wu.status='ACTIVE' AND w.status='ACTIVE' AND c.status='ACTIVE' AND u.status='ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `SELECT c.type,COALESCE(cu.role,'') FROM channels c LEFT JOIN channel_users cu ON cu.channel_id=c.id AND cu.user_id=$2 WHERE c.id=$1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var id string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id=$1 AND user_id=$2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&id) == nil
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
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID+"#message-"+message.ID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID+"#message-"+message.ID)
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
		if err := db.DB.QueryRowContext(r.Context(), `SELECT ch.workspace_id,ch.channel_id,ch.type,wu.role FROM chats ch JOIN workspaces w ON w.id=ch.workspace_id JOIN companies c ON c.id=w.company_id JOIN workspace_users wu ON wu.workspace_id=ch.workspace_id AND wu.user_id=$2 JOIN company_users company_member ON company_member.company_id=w.company_id AND company_member.user_id=$2 JOIN users u ON u.id=$2 WHERE ch.id=$1 AND wu.status='ACTIVE' AND w.status='ACTIVE' AND c.status='ACTIVE' AND u.status='ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		allowed := false
		channelRole := ""
		if chatType == "CHANNEL" && channelID != nil {
			var channelType string
			if err := db.DB.QueryRowContext(r.Context(), `SELECT c.type,COALESCE(cu.role,'') FROM channels c LEFT JOIN channel_users cu ON cu.channel_id=c.id AND cu.user_id=$2 WHERE c.id=$1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var id string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id=$1 AND user_id=$2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&id) == nil
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
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID+"#message-"+message.ID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID+"#message-"+message.ID)
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
		if err := db.DB.QueryRowContext(r.Context(), `SELECT ch.workspace_id,ch.channel_id,ch.type,wu.role FROM chats ch JOIN workspaces w ON w.id=ch.workspace_id JOIN companies c ON c.id=w.company_id JOIN workspace_users wu ON wu.workspace_id=ch.workspace_id AND wu.user_id=$2 JOIN company_users company_member ON company_member.company_id=w.company_id AND company_member.user_id=$2 JOIN users u ON u.id=$2 WHERE ch.id=$1 AND wu.status='ACTIVE' AND w.status='ACTIVE' AND c.status='ACTIVE' AND u.status='ACTIVE'`, message.ChatID, session.ID).Scan(&workspaceID, &channelID, &chatType, &workspaceRole); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		allowed := false
		if chatType == "CHANNEL" && channelID != nil {
			var channelType, channelRole string
			if err := db.DB.QueryRowContext(r.Context(), `SELECT c.type,COALESCE(cu.role,'') FROM channels c LEFT JOIN channel_users cu ON cu.channel_id=c.id AND cu.user_id=$2 WHERE c.id=$1`, *channelID, session.ID).Scan(&channelType, &channelRole); err == nil {
				allowed = channelRole != "" || (channelType == "PUBLIC" && workspaceRole != "GUEST")
			}
		} else {
			var id string
			allowed = db.DB.QueryRowContext(r.Context(), `SELECT id FROM chat_users WHERE chat_id=$1 AND user_id=$2 AND left_at IS NULL`, message.ChatID, session.ID).Scan(&id) == nil
		}
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		reactionValue := strings.TrimSpace(r.FormValue("reaction"))
		if reactionValue == "" || len([]rune(reactionValue)) > 20 {
			http.Error(w, "invalid reaction", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		var reactionID string
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM chat_messages_reactions WHERE chat_message_id=$1 AND user_id=$2 AND reaction=$3 FOR UPDATE`, message.ID, session.ID, reactionValue).Scan(&reactionID)
		added := false
		if err == nil {
			if _, err := tx.ExecContext(r.Context(), `DELETE FROM chat_messages_reactions WHERE id=$1`, reactionID); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not remove reaction", http.StatusInternalServerError)
				return
			}
		} else if err == sql.ErrNoRows {
			reaction := models.ChatMessageReaction{ChatMessageID: message.ID, UserID: session.ID, Reaction: reactionValue}
			if err := reaction.Create(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not add reaction", http.StatusInternalServerError)
				return
			}
			added = true
		} else {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not check reaction", http.StatusInternalServerError)
			return
		}
		if added && message.UserID != session.ID {
			var reactorName string
			_ = tx.QueryRowContext(r.Context(), `SELECT name FROM users WHERE id=$1`, session.ID).Scan(&reactorName)
			if reactorName == "" {
				reactorName = "Someone"
			}
			title := "New reaction"
			actionURL := "/workspaces/" + workspaceID + "?chat=" + message.ChatID + "#message-" + message.ID
			if channelID != nil {
				actionURL = "/workspaces/" + workspaceID + "?channel=" + *channelID + "#message-" + message.ID
			}
			n := models.UserNotification{UserID: message.UserID, Type: "MESSAGE_REACTION", Title: &title, Content: reactorName + " reacted " + reactionValue + " to your message.", ActionURL: &actionURL}
			if err := n.Create(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not create notification", http.StatusInternalServerError)
				return
			}
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update reaction", http.StatusInternalServerError)
			return
		}
		if channelID != nil {
			utils.Redirect(w, r, "/workspaces/"+workspaceID+"?channel="+*channelID+"#message-"+message.ID)
			return
		}
		utils.Redirect(w, r, "/workspaces/"+workspaceID+"?chat="+message.ChatID+"#message-"+message.ID)
	}))
}
