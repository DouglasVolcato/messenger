package routes

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

func loadCurrentUser(w http.ResponseWriter, r *http.Request) (models.User, bool) {
	session, err := utils.GetUserFromCookie(r)
	if err != nil {
		utils.Redirect(w, r, "/login")
		return models.User{}, false
	}
	if user, err := cache.GetUserCache(r.Context(), session.ID); err == nil {
		if user.Status == "ACTIVE" {
			return *user, true
		}
		utils.ClearUserCookie(w, r)
		utils.Redirect(w, r, "/login")
		return models.User{}, false
	}
	user := models.User{ID: session.ID}
	if err := user.GetOne(db.DB, r.Context()); err != nil || user.Status != "ACTIVE" {
		utils.ClearUserCookie(w, r)
		utils.Redirect(w, r, "/login")
		return models.User{}, false
	}
	_ = cache.SetUserCache(r.Context(), user)
	return user, true
}

func loadCompanyForUser(r *http.Request, companyID, userID string) (models.Company, string, error) {
	var company *models.Company
	if cached, err := cache.GetCompanyCache(r.Context(), companyID); err == nil {
		company = cached
	}
	if company == nil {
		loaded := models.Company{ID: companyID}
		if err := loaded.GetOne(db.DB, r.Context()); err != nil {
			return models.Company{}, "", err
		}
		_ = cache.SetCompanyCache(r.Context(), loaded)
		company = &loaded
	}
	if company.Status != "ACTIVE" {
		return models.Company{}, "", sql.ErrNoRows
	}
	var role string
	err := db.DB.QueryRowContext(r.Context(), `
		SELECT cu.role
		FROM company_users cu
		WHERE cu.company_id = $1 AND cu.user_id = $2`,
		companyID, userID).Scan(&role)
	if err != nil {
		return models.Company{}, "", err
	}
	return *company, role, nil
}

func companyChatsCacheKey(companyID string) string {
	return fmt.Sprintf("company:%s:chats", companyID)
}

func messagesCacheKey(prefix string, firstID, secondID string, limit int) string {
	ids := []string{firstID, secondID}
	sort.Strings(ids)
	return fmt.Sprintf("messages:%s:%s:%s:%d", prefix, ids[0], ids[1], limit)
}

func deleteMessageCache(ctx context.Context, message models.Message) {
	if message.ChatID != nil {
		_ = cache.DeleteManyMessagesCache(ctx, messagesCacheKey("chat", *message.ChatID, "", 100))
		return
	}
	if message.RecipientUserID != nil {
		_ = cache.DeleteManyMessagesCache(ctx, messagesCacheKey("direct", message.SenderUserID, *message.RecipientUserID, 100))
	}
}

func invalidateUserCompanyCaches(ctx context.Context, userID string) {
	rows, err := db.DB.QueryContext(ctx, `SELECT company_id FROM company_users WHERE user_id = $1`, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var companyID string
		if rows.Scan(&companyID) == nil {
			_ = cache.DeleteCompanyUsersCache(ctx, companyID)
		}
	}
}

func requireCompanyMember(w http.ResponseWriter, r *http.Request, companyID, userID string) (models.Company, string, bool) {
	company, role, err := loadCompanyForUser(r, companyID, userID)
	if err == sql.ErrNoRows {
		http.Error(w, "forbidden", http.StatusForbidden)
		return models.Company{}, "", false
	}
	if err != nil {
		http.Error(w, "could not load company", http.StatusInternalServerError)
		return models.Company{}, "", false
	}
	return company, role, true
}

func requireCompanyAdmin(w http.ResponseWriter, r *http.Request, companyID, userID string) (models.Company, bool) {
	company, role, ok := requireCompanyMember(w, r, companyID, userID)
	if !ok {
		return models.Company{}, false
	}
	if role != "ADMIN" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return models.Company{}, false
	}
	return company, true
}

func unreadNotifications(userID string, r *http.Request) int64 {
	var count int64
	if err := db.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM user_notifications
		WHERE user_id = $1 AND is_read = FALSE`, userID,
	).Scan(&count); err != nil {
		return 0
	}
	return count
}

func safeReturnURL(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return fallback
	}
	return value
}

func usersShareCompany(r *http.Request, firstUserID, secondUserID string) bool {
	var companyID string
	return db.DB.QueryRowContext(r.Context(), `
		SELECT a.company_id
		FROM company_users a
		JOIN company_users b ON b.company_id = a.company_id
		JOIN companies c ON c.id = a.company_id AND c.status = 'ACTIVE'
		WHERE a.user_id = $1 AND b.user_id = $2
		LIMIT 1`,
		firstUserID, secondUserID,
	).Scan(&companyID) == nil
}

func messageVisibleToUser(r *http.Request, message models.Message, userID string) bool {
	if message.Direct {
		if message.RecipientUserID == nil || (message.SenderUserID != userID && *message.RecipientUserID != userID) {
			return false
		}
		otherUserID := message.SenderUserID
		if otherUserID == userID {
			otherUserID = *message.RecipientUserID
		}
		return usersShareCompany(r, userID, otherUserID)
	}
	if message.ChatID == nil {
		return false
	}
	var membershipID string
	return db.DB.QueryRowContext(r.Context(), `
		SELECT cu.id
		FROM chat_users cu
		JOIN chats ch ON ch.id = cu.chat_id
		JOIN company_users company_member
		  ON company_member.company_id = ch.company_id
		 AND company_member.user_id = cu.user_id
		WHERE cu.chat_id = $1 AND cu.user_id = $2`,
		*message.ChatID, userID,
	).Scan(&membershipID) == nil
}
