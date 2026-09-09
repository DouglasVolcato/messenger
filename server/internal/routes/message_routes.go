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
	"github.com/lib/pq"
)

func loadMessageViews(r *http.Request, messages []models.Message, currentUserID string) ([]MessageView, error) {
	if len(messages) == 0 {
		return []MessageView{}, nil
	}

	userIDs := make([]string, 0, len(messages))
	messageIDs := make([]string, 0, len(messages))
	seenUsers := make(map[string]bool)
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
		if !seenUsers[message.SenderUserID] {
			seenUsers[message.SenderUserID] = true
			userIDs = append(userIDs, message.SenderUserID)
		}
	}

	names := make(map[string]string)
	rows, err := db.DB.QueryContext(r.Context(), `
		SELECT id, name FROM users WHERE id::text = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	reactions := make(map[string][]ReactionView)
	reactionRows, err := db.DB.QueryContext(r.Context(), `
		SELECT message_id, reaction, COUNT(*)::BIGINT,
		       BOOL_OR(user_id = $2)
		FROM messages_reactions
		WHERE message_id::text = ANY($1)
		GROUP BY message_id, reaction
		ORDER BY message_id, reaction`, pq.Array(messageIDs), currentUserID)
	if err != nil {
		return nil, err
	}
	for reactionRows.Next() {
		var messageID string
		var reaction ReactionView
		if err := reactionRows.Scan(&messageID, &reaction.Reaction, &reaction.Count, &reaction.Reacted); err != nil {
			reactionRows.Close()
			return nil, err
		}
		reactions[messageID] = append(reactions[messageID], reaction)
	}
	if err := reactionRows.Err(); err != nil {
		reactionRows.Close()
		return nil, err
	}
	reactionRows.Close()

	items := make([]MessageView, 0, len(messages))
	for _, message := range messages {
		items = append(items, MessageView{
			ID:        message.ID,
			UserID:    message.SenderUserID,
			UserName:  names[message.SenderUserID],
			Content:   message.Content,
			Direct:    message.Direct,
			CreatedAt: message.CreatedAt,
			EditedAt:  message.EditedAt,
			DeletedAt: message.DeletedAt,
			Reactions: reactions[message.ID],
		})
	}
	return items, nil
}

func RegisterMessageRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	mux.Handle("GET /companies/{companyID}/chats/{chatID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		companyID := r.PathValue("companyID")
		company, role, ok := requireCompanyMember(w, r, companyID, user.ID)
		if !ok {
			return
		}

		chat := models.Chat{ID: r.PathValue("chatID")}
		if err := chat.GetOne(db.DB, r.Context()); err != nil || chat.CompanyID != companyID {
			http.NotFound(w, r)
			return
		}
		subscription := models.ChatUser{ChatID: chat.ID, UserID: user.ID}
		if err := subscription.GetOneByChatAndUser(db.DB, r.Context()); err != nil {
			http.Error(w, "subscribe to this chat before reading its messages", http.StatusForbidden)
			return
		}

		messageModel := models.Message{ChatID: &chat.ID}
		messages, err := messageModel.GetChatMessages(db.DB, r.Context(), 100)
		if err != nil {
			http.Error(w, "could not load messages", http.StatusInternalServerError)
			return
		}
		messageViews, err := loadMessageViews(r, messages, user.ID)
		if err != nil {
			http.Error(w, "could not load messages", http.StatusInternalServerError)
			return
		}
		var memberCount int64
		_ = db.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM chat_users WHERE chat_id = $1`, chat.ID).Scan(&memberCount)

		clientMessageID, _ := utils.GenerateUUID()
		utils.ExecuteTemplate(w, templ, "chats/show.html", &ViewData{
			AppVersion:          appVersion,
			User:                user,
			Company:             company,
			CompanyRole:         role,
			CanManage:           role == "ADMIN",
			CurrentChat:         ChatView{ID: chat.ID, CompanyID: companyID, Name: chat.Name, Subscribed: true, MemberCount: memberCount},
			Messages:            messageViews,
			BackURL:             "/companies/" + companyID,
			ClientMessageID:     clientMessageID,
			UnreadNotifications: unreadNotifications(user.ID, r),
			Success:             r.URL.Query().Get("success"),
			Error:               r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/chats/{chatID}/messages", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		chat := models.Chat{ID: r.PathValue("chatID")}
		if err := chat.GetOne(db.DB, r.Context()); err != nil {
			http.NotFound(w, r)
			return
		}
		if _, _, ok := requireCompanyMember(w, r, chat.CompanyID, user.ID); !ok {
			return
		}
		subscription := models.ChatUser{ChatID: chat.ID, UserID: user.ID}
		if err := subscription.GetOneByChatAndUser(db.DB, r.Context()); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		content := strings.TrimSpace(r.FormValue("content"))
		if content == "" {
			utils.Redirect(w, r, "/companies/"+chat.CompanyID+"/chats/"+chat.ID+"?error=Message+cannot+be+empty")
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		message := models.Message{
			ChatID:          &chat.ID,
			SenderUserID:    user.ID,
			ClientMessageID: strings.TrimSpace(r.FormValue("client_message_id")),
			Content:         content,
		}
		if err := message.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			if message.ClientMessageID != "" {
				existing := models.Message{ClientMessageID: message.ClientMessageID}
				if existing.GetOneByClientMessageID(db.DB, r.Context()) == nil && existing.SenderUserID == user.ID {
					if !existing.Direct && existing.ChatID != nil && *existing.ChatID == chat.ID && existing.Content == content {
						utils.Redirect(w, r, "/companies/"+chat.CompanyID+"/chats/"+chat.ID+"#message-"+existing.ID)
						return
					}
					http.Error(w, "client_message_id is already used for another message", http.StatusConflict)
					return
				}
			}
			http.Error(w, "could not send message", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not send message", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/companies/"+chat.CompanyID+"/chats/"+chat.ID+"#message-"+message.ID)
	}))

	mux.Handle("GET /messages/users/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		target := models.User{ID: r.PathValue("userID")}
		if err := target.GetOne(db.DB, r.Context()); err != nil || target.Status != "ACTIVE" || target.ID == user.ID {
			http.NotFound(w, r)
			return
		}
		if !usersShareCompany(r, user.ID, target.ID) {
			http.Error(w, "you can only message users who share a company with you", http.StatusForbidden)
			return
		}

		var companyID string
		_ = db.DB.QueryRowContext(r.Context(), `
			SELECT a.company_id
			FROM company_users a
			JOIN company_users b ON b.company_id = a.company_id
			WHERE a.user_id = $1 AND b.user_id = $2
			ORDER BY a.created_at ASC
			LIMIT 1`, user.ID, target.ID).Scan(&companyID)

		messageModel := models.Message{SenderUserID: user.ID}
		messages, err := messageModel.GetDirectMessages(db.DB, r.Context(), target.ID, 100)
		if err != nil {
			http.Error(w, "could not load direct messages", http.StatusInternalServerError)
			return
		}
		messageViews, err := loadMessageViews(r, messages, user.ID)
		if err != nil {
			http.Error(w, "could not load direct messages", http.StatusInternalServerError)
			return
		}
		clientMessageID, _ := utils.GenerateUUID()
		backURL := "/companies"
		if companyID != "" {
			backURL = "/companies/" + companyID
		}
		utils.ExecuteTemplate(w, templ, "messages/direct.html", &ViewData{
			AppVersion:          appVersion,
			User:                user,
			DirectUser:          target,
			Messages:            messageViews,
			BackURL:             backURL,
			ClientMessageID:     clientMessageID,
			UnreadNotifications: unreadNotifications(user.ID, r),
			Success:             r.URL.Query().Get("success"),
			Error:               r.URL.Query().Get("error"),
		})
	}))

	mux.Handle("POST /api/messages/users/{userID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		target := models.User{ID: r.PathValue("userID")}
		if err := target.GetOne(db.DB, r.Context()); err != nil || target.Status != "ACTIVE" || target.ID == user.ID {
			http.NotFound(w, r)
			return
		}
		if !usersShareCompany(r, user.ID, target.ID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		content := strings.TrimSpace(r.FormValue("content"))
		if content == "" {
			utils.Redirect(w, r, "/messages/users/"+target.ID+"?error=Message+cannot+be+empty")
			return
		}

		tx, err := db.BeginTransaction(r.Context())
		if err != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		message := models.Message{
			SenderUserID:    user.ID,
			RecipientUserID: &target.ID,
			Direct:          true,
			ClientMessageID: strings.TrimSpace(r.FormValue("client_message_id")),
			Content:         content,
		}
		if err := message.Create(tx, r.Context()); err != nil {
			_ = db.RollbackTransaction(tx)
			if message.ClientMessageID != "" {
				existing := models.Message{ClientMessageID: message.ClientMessageID}
				if existing.GetOneByClientMessageID(db.DB, r.Context()) == nil && existing.SenderUserID == user.ID {
					if existing.Direct && existing.RecipientUserID != nil && *existing.RecipientUserID == target.ID && existing.Content == content {
						utils.Redirect(w, r, "/messages/users/"+target.ID+"#message-"+existing.ID)
						return
					}
					http.Error(w, "client_message_id is already used for another message", http.StatusConflict)
					return
				}
			}
			http.Error(w, "could not send message", http.StatusBadRequest)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not send message", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, "/messages/users/"+target.ID+"#message-"+message.ID)
	}))

	mux.Handle("PATCH /api/messages/{messageID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		message := models.Message{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.SenderUserID != user.ID || message.DeletedAt != nil {
			http.NotFound(w, r)
			return
		}
		if !messageVisibleToUser(r, message, user.ID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		content := strings.TrimSpace(r.FormValue("content"))
		if content == "" {
			http.Error(w, "message cannot be empty", http.StatusBadRequest)
			return
		}
		message.Content = content
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
		utils.Redirect(w, r, safeReturnURL(r.FormValue("return_to"), "/companies")+"#message-"+message.ID)
	}))

	mux.Handle("DELETE /api/messages/{messageID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		message := models.Message{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.SenderUserID != user.ID || message.DeletedAt != nil {
			http.NotFound(w, r)
			return
		}
		if !messageVisibleToUser(r, message, user.ID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		now := time.Now()
		message.DeletedAt = &now
		message.Content = ""

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
		utils.Redirect(w, r, safeReturnURL(r.URL.Query().Get("return_to"), "/companies"))
	}))

	mux.Handle("POST /api/messages/reactions/{messageID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := loadCurrentUser(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		message := models.Message{ID: r.PathValue("messageID")}
		if err := message.GetOne(db.DB, r.Context()); err != nil || message.DeletedAt != nil {
			http.NotFound(w, r)
			return
		}
		if !messageVisibleToUser(r, message, user.ID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		reactionValue := strings.TrimSpace(r.FormValue("reaction"))
		if reactionValue == "" || len([]rune(reactionValue)) > 16 {
			http.Error(w, "invalid reaction", http.StatusBadRequest)
			return
		}

		var existingID string
		err := db.DB.QueryRowContext(r.Context(), `
			SELECT id FROM messages_reactions
			WHERE message_id = $1 AND user_id = $2 AND reaction = $3`,
			message.ID, user.ID, reactionValue,
		).Scan(&existingID)

		tx, txErr := db.BeginTransaction(r.Context())
		if txErr != nil {
			http.Error(w, "could not start transaction", http.StatusInternalServerError)
			return
		}
		reaction := models.MessageReaction{MessageID: message.ID, UserID: user.ID, Reaction: reactionValue}
		if err == nil {
			if err := reaction.Delete(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not remove reaction", http.StatusInternalServerError)
				return
			}
		} else if err == sql.ErrNoRows {
			if err := reaction.Create(tx, r.Context()); err != nil {
				_ = db.RollbackTransaction(tx)
				http.Error(w, "could not add reaction", http.StatusInternalServerError)
				return
			}
		} else {
			_ = db.RollbackTransaction(tx)
			http.Error(w, "could not check reaction", http.StatusInternalServerError)
			return
		}
		if err := db.CommitTransaction(tx); err != nil {
			http.Error(w, "could not update reaction", http.StatusInternalServerError)
			return
		}
		utils.Redirect(w, r, safeReturnURL(r.FormValue("return_to"), "/companies")+"#message-"+message.ID)
	}))
}
