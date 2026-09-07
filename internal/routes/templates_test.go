package routes

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
)

var splitContextSensitiveEndTag = regexp.MustCompile(`(?i)</(?:textarea|title|style|script)\s+>`)

func TestViewsExecuteWithoutContextErrors(t *testing.T) {
	viewsDir := filepath.Join("..", "views")
	templ := template.New("")
	var pageTemplates []string

	err := filepath.Walk(viewsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if splitContextSensitiveEndTag.Match(content) {
			return fmt.Errorf("context-sensitive HTML end tag is split by whitespace in %s", path)
		}

		name := strings.TrimPrefix(path, viewsDir+string(os.PathSeparator)+"pages"+string(os.PathSeparator))
		if strings.HasPrefix(path, filepath.Join(viewsDir, "components")+string(os.PathSeparator)) {
			name = strings.TrimPrefix(path, viewsDir+string(os.PathSeparator))
		} else {
			pageTemplates = append(pageTemplates, name)
		}

		_, err = templ.New(name).Parse(string(content))
		return err
	})
	if err != nil {
		t.Fatalf("validate views: %v", err)
	}

	assertLayoutFragments(t, viewsDir)

	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	description := "Description"
	title := "Notification"
	actionURL := "/workspaces/workspace-1"

	workspace := WorkspaceView{
		ID:          "workspace-1",
		CompanyID:   "company-1",
		CompanyName: "Company",
		Name:        "Workspace",
		Slug:        "workspace",
		Status:      "ACTIVE",
		Role:        "OWNER",
		CompanyRole: "OWNER",
	}
	chat := ChatView{
		ID:          "chat-1",
		ChannelID:   "channel-1",
		Name:        "General",
		Description: "General channel",
		Type:        "CHANNEL",
	}
	member := MemberView{
		ID:       "member-1",
		UserID:   "user-1",
		Name:     "User",
		Username: "user",
		Email:    "user@example.com",
		Role:     "OWNER",
		Status:   "ACTIVE",
	}

	richData := ViewData{
		AppVersion: "test",
		Error:      "error",
		Success:    "success",
		BackURL:    "/",
		User: models.User{
			ID:          "user-1",
			Name:        "User",
			Username:    "user",
			Email:       "user@example.com",
			Status:      "ACTIVE",
			SystemAdmin: true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Company: models.Company{
			ID:        "company-1",
			Name:      "Company",
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
		Workspace: models.Workspace{
			ID:        "workspace-1",
			CompanyID: "company-1",
			Name:      "Workspace",
			Slug:      "workspace",
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
		Channel: models.Channel{
			ID:          "channel-1",
			WorkspaceID: "workspace-1",
			Name:        "general",
			Description: &description,
			Type:        "PUBLIC",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Role:                "OWNER",
		CompanyRole:         "OWNER",
		CanManage:           true,
		WorkspaceContextID:  "workspace-1",
		UnreadNotifications: 1,
		Workspaces:           []WorkspaceView{workspace},
		CompanyWorkspaces: []CompanyWorkspaceView{{
			ID:         "company-1",
			Name:       "Company",
			Status:     "ACTIVE",
			Role:       "OWNER",
			CanManage:  true,
			Workspaces: []WorkspaceView{workspace},
		}},
		Channels: []ChannelView{{
			ID:          "channel-1",
			WorkspaceID: "workspace-1",
			Name:        "general",
			Description: "General channel",
			Type:        "PUBLIC",
			MemberCount: 1,
		}},
		Members: []MemberView{member},
		Member:  member,
		Notifications: []models.UserNotification{{
			ID:        "notification-1",
			UserID:    "user-1",
			Type:      "MESSAGE",
			Title:     &title,
			Content:   "New message",
			ActionURL: &actionURL,
			CreatedAt: now,
			UpdatedAt: now,
		}},
		Chats:       []ChatView{chat},
		CurrentChat: chat,
		Messages: []MessageView{{
			ID:        "message-1",
			UserID:    "user-1",
			UserName:  "User",
			Content:   "Message content",
			Type:      "TEXT",
			CreatedAt: now,
			EditedAt:  &now,
			Reactions: []ReactionView{{Reaction: "👍", Count: 1, Reacted: true}},
		}},
		AdminCompanies: []AdminCompanyView{{
			ID:        "company-1",
			Name:      "Company",
			Status:    "ACTIVE",
			UserCount: 1,
			CreatedAt: now,
		}},
		AdminUsers: []AdminUserView{{
			ID:           "user-1",
			Name:         "User",
			Username:     "user",
			Email:        "user@example.com",
			Status:       "ACTIVE",
			SystemAdmin:  true,
			CompanyCount: 1,
			CreatedAt:    now,
		}},
		UserCompanies: []AdminUserCompanyView{{
			CompanyID:   "company-1",
			CompanyName: "Company",
			Role:        "OWNER",
		}},
		Page:         2,
		PrevPage:     1,
		NextPage:     3,
		Limit:        20,
		Total:        21,
		HasPrev:      true,
		HasNext:      true,
		Identifier:   "user",
		Name:         "User",
		Username:     "user",
		Email:        "user@example.com",
		Query:        "user",
		StatusFilter: "ACTIVE",
	}

	cases := []struct {
		name string
		data ViewData
	}{
		{name: "empty", data: ViewData{}},
		{name: "populated", data: richData},
	}

	for _, templateName := range pageTemplates {
		for _, tc := range cases {
			t.Run(templateName+"/"+tc.name, func(t *testing.T) {
				data := tc.data
				if err := templ.ExecuteTemplate(io.Discard, templateName, &data); err != nil {
					t.Fatalf("execute template %q: %v", templateName, err)
				}
			})
		}
	}
}

func assertLayoutFragments(t *testing.T, viewsDir string) {
	t.Helper()

	top, err := os.ReadFile(filepath.Join(viewsDir, "components", "top.html"))
	if err != nil {
		t.Fatalf("read top layout fragment: %v", err)
	}
	bottom, err := os.ReadFile(filepath.Join(viewsDir, "components", "bottom.html"))
	if err != nil {
		t.Fatalf("read bottom layout fragment: %v", err)
	}

	topHTML := strings.ToLower(string(top))
	bottomHTML := strings.ToLower(string(bottom))

	if !strings.Contains(topHTML, "<body>") {
		t.Fatal("top layout fragment must open <body>")
	}
	if strings.Contains(topHTML, "</body>") || strings.Contains(topHTML, "</html>") {
		t.Fatal("top layout fragment must not close </body> or </html>")
	}
	if !strings.Contains(bottomHTML, "</body>") || !strings.Contains(bottomHTML, "</html>") {
		t.Fatal("bottom layout fragment must close </body> and </html>")
	}
}
