package tester

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type generatedUser struct {
	ID       string
	Name     string
	Username string
	Email    string
	Password string
	Token    string
}

type responseMeta struct {
	Status   int
	Location string
	Cookies  []*http.Cookie
}

type messageRef struct {
	ID      string
	OwnerID string
	Deleted bool
}

func (r *Runner) request(ctx context.Context, operation, method, path, token string, form url.Values) (responseMeta, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, r.cfg.BaseURL+path, body)
	if err != nil {
		r.metrics.ObserveHTTP(operation, 0, 0)
		return responseMeta{}, err
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		request.Header.Set("Cookie", "user="+token)
	}

	started := time.Now()
	response, err := r.httpClient.Do(request)
	elapsed := time.Since(started)
	if err != nil {
		r.metrics.ObserveHTTP(operation, 0, elapsed)
		return responseMeta{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))

	r.metrics.ObserveHTTP(operation, response.StatusCode, elapsed)
	return responseMeta{
		Status:   response.StatusCode,
		Location: response.Header.Get("Location"),
		Cookies:  response.Cookies(),
	}, nil
}

func (r *Runner) registerUser(ctx context.Context) (generatedUser, error) {
	suffix := fmt.Sprintf("%d-%08x", time.Now().UnixNano(), r.random.Uint32())
	username := "load-" + suffix
	user := generatedUser{
		Name:     "Load Test " + suffix,
		Username: username,
		Email:    username + "@example.test",
		Password: r.cfg.Password,
	}

	meta, err := r.request(ctx, "register", http.MethodPost, "/api/auth/register", "", url.Values{
		"name":             {user.Name},
		"username":         {user.Username},
		"email":            {user.Email},
		"password":         {user.Password},
		"confirm_password": {user.Password},
	})
	if err != nil {
		return generatedUser{}, err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return generatedUser{}, fmt.Errorf("register returned HTTP %d", meta.Status)
	}
	for _, cookie := range meta.Cookies {
		if cookie.Name == "user" {
			user.Token = cookie.Value
			break
		}
	}
	if user.Token == "" {
		return generatedUser{}, fmt.Errorf("register response did not set user cookie")
	}
	user.ID, err = userIDFromToken(user.Token)
	if err != nil {
		return generatedUser{}, fmt.Errorf("decode registered user id: %w", err)
	}
	return user, nil
}

func (r *Runner) login(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "login", http.MethodPost, "/api/auth/login", "", url.Values{
		"identifier": {user.Username},
		"password":   {user.Password},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("login returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) createCompany(ctx context.Context, admin generatedUser) (string, error) {
	meta, err := r.request(ctx, "company_create", http.MethodPost, "/api/companies", admin.Token, url.Values{
		"name": {"Load Test Company " + randomID()},
	})
	if err != nil {
		return "", err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return "", fmt.Errorf("create company returned HTTP %d", meta.Status)
	}
	id := pathValue(meta.Location, "companies")
	if id == "" {
		return "", fmt.Errorf("could not extract company id from redirect %q", meta.Location)
	}
	return id, nil
}

func (r *Runner) addMember(ctx context.Context, admin generatedUser, companyID string, member generatedUser) error {
	meta, err := r.request(ctx, "company_member_add", http.MethodPost, "/api/companies/"+companyID+"/members", admin.Token, url.Values{
		"identifier": {member.Username},
		"role":       {"USER"},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("add member returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) createChat(ctx context.Context, admin generatedUser, companyID string) (string, error) {
	meta, err := r.request(ctx, "chat_create", http.MethodPost, "/api/companies/"+companyID+"/chats", admin.Token, url.Values{
		"name": {"Load Test Chat"},
	})
	if err != nil {
		return "", err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return "", fmt.Errorf("create chat returned HTTP %d", meta.Status)
	}
	id := pathValue(meta.Location, "chats")
	if id == "" {
		return "", fmt.Errorf("could not extract chat id from redirect %q", meta.Location)
	}
	return id, nil
}

func (r *Runner) subscribe(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "chat_subscribe", http.MethodPost,
		"/api/companies/"+r.companyID+"/chats/"+r.chatID+"/subscribe", user.Token, nil)
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("subscribe returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) sendChatMessage(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "chat_message", http.MethodPost, "/api/chats/"+r.chatID+"/messages", user.Token, url.Values{
		"client_message_id": {randomID()},
		"content":           {r.randomMessage()},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("chat message returned HTTP %d", meta.Status)
	}
	if messageID := messageIDFromLocation(meta.Location); messageID != "" {
		r.messagesMu.Lock()
		r.chatMessages = append(r.chatMessages, messageRef{ID: messageID, OwnerID: user.ID})
		r.messagesMu.Unlock()
	}
	r.metrics.IncGeneratedMessage("chat")
	return nil
}

func (r *Runner) sendDirectMessage(ctx context.Context, sender, recipient generatedUser) error {
	meta, err := r.request(ctx, "direct_message", http.MethodPost, "/api/messages/users/"+recipient.ID, sender.Token, url.Values{
		"client_message_id": {randomID()},
		"content":           {r.randomMessage()},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("direct message returned HTTP %d", meta.Status)
	}
	r.metrics.IncGeneratedMessage("direct")
	return nil
}

func (r *Runner) reactToMessage(ctx context.Context, user generatedUser) error {
	message, ok := r.randomChatMessage("")
	if !ok {
		return nil
	}
	reactions := []string{"👍", "🔥", "✅", "🚀"}
	meta, err := r.request(ctx, "message_reaction", http.MethodPost, "/api/messages/reactions/"+message.ID, user.Token, url.Values{
		"reaction":  {reactions[r.random.Intn(len(reactions))]},
		"return_to": {"/companies/" + r.companyID + "/chats/" + r.chatID},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("message reaction returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) editOwnMessage(ctx context.Context, user generatedUser) error {
	message, ok := r.randomChatMessage(user.ID)
	if !ok {
		return nil
	}
	meta, err := r.request(ctx, "message_edit", http.MethodPatch, "/api/messages/"+message.ID, user.Token, url.Values{
		"content":   {"edited " + r.randomMessage()},
		"return_to": {"/companies/" + r.companyID + "/chats/" + r.chatID},
	})
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("message edit returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) deleteOwnMessage(ctx context.Context, user generatedUser) error {
	message, ok := r.randomChatMessage(user.ID)
	if !ok {
		return nil
	}
	returnTo := url.QueryEscape("/companies/" + r.companyID + "/chats/" + r.chatID)
	meta, err := r.request(ctx, "message_delete", http.MethodDelete, "/api/messages/"+message.ID+"?return_to="+returnTo, user.Token, nil)
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("message delete returned HTTP %d", meta.Status)
	}
	r.messagesMu.Lock()
	for index := range r.chatMessages {
		if r.chatMessages[index].ID == message.ID {
			r.chatMessages[index].Deleted = true
			break
		}
	}
	r.messagesMu.Unlock()
	return nil
}

func (r *Runner) readChat(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "chat_read", http.MethodGet,
		"/companies/"+r.companyID+"/chats/"+r.chatID, user.Token, nil)
	if err != nil {
		return err
	}
	if meta.Status != http.StatusOK {
		return fmt.Errorf("chat read returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) readCompanies(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "companies_read", http.MethodGet, "/companies", user.Token, nil)
	if err != nil {
		return err
	}
	if meta.Status != http.StatusOK {
		return fmt.Errorf("companies read returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) readNotifications(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "notifications_read", http.MethodGet, "/notifications", user.Token, nil)
	if err != nil {
		return err
	}
	if meta.Status != http.StatusOK {
		return fmt.Errorf("notifications read returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) markNotificationsRead(ctx context.Context, user generatedUser) error {
	meta, err := r.request(ctx, "notifications_read_all", http.MethodPost, "/api/notifications/read-all", user.Token, nil)
	if err != nil {
		return err
	}
	if !isSuccessOrRedirect(meta.Status) {
		return fmt.Errorf("notifications read-all returned HTTP %d", meta.Status)
	}
	return nil
}

func (r *Runner) randomChatMessage(ownerID string) (messageRef, bool) {
	r.messagesMu.RLock()
	defer r.messagesMu.RUnlock()

	candidates := make([]messageRef, 0, len(r.chatMessages))
	for _, message := range r.chatMessages {
		if message.Deleted {
			continue
		}
		if ownerID != "" && message.OwnerID != ownerID {
			continue
		}
		candidates = append(candidates, message)
	}
	if len(candidates) == 0 {
		return messageRef{}, false
	}
	return candidates[r.random.Intn(len(candidates))], true
}

func (r *Runner) randomMessage() string {
	words := []string{
		"architecture", "message", "distributed", "latency", "queue", "websocket",
		"cache", "worker", "replica", "database", "throughput", "notification",
	}
	count := 5 + r.random.Intn(20)
	result := make([]string, count)
	for index := range result {
		result[index] = words[r.random.Intn(len(words))]
	}
	return strings.Join(result, " ") + " " + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func userIDFromToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("unexpected JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	if claims.UserID == "" {
		return "", fmt.Errorf("JWT has no user_id claim")
	}
	return claims.UserID, nil
}

func pathValue(location, segment string) string {
	if location == "" {
		return ""
	}
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == segment {
			return parts[index+1]
		}
	}
	return ""
}

func messageIDFromLocation(location string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Fragment, "message-")
}

func isSuccessOrRedirect(status int) bool {
	return status >= 200 && status < 400
}

func randomID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}

