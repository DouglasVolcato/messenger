package routes

import (
	"html/template"
	"net/http"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
)

type ViewData struct {
	AppVersion string
	Error      string

	User      models.User
	Company   models.Company
	Workspace models.Workspace
	Channel   models.Channel

	Role        string
	CompanyRole string
	CanManage   bool

	Workspaces    []WorkspaceView
	Channels      []ChannelView
	Members       []MemberView
	Member        MemberView
	Notifications []models.UserNotification
	Chats         []ChatView
	Messages      []MessageView
	CurrentChat   ChatView

	Page     int
	PrevPage int
	NextPage int
	Limit    int
	Total    int64
	HasPrev  bool
	HasNext  bool

	Identifier string
	Name       string
	Username   string
	Email      string
}

type WorkspaceView struct {
	ID          string
	CompanyID   string
	CompanyName string
	Name        string
	Slug        string
	Status      string
	Role        string
	CompanyRole string
}

type ChannelView struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	Type        string
	MemberCount int64
}

type ChatView struct {
	ID          string
	ChannelID   string
	Name        string
	Description string
	Type        string
}

type MessageView struct {
	ID        string
	UserID    string
	UserName  string
	Content   string
	Type      string
	CreatedAt time.Time
	EditedAt  *time.Time
	DeletedAt *time.Time
}

type MemberView struct {
	ID       string
	UserID   string
	Name     string
	Username string
	Email    string
	Role     string
	Status   string
}

func RegisterRoutes(mux *http.ServeMux, templ *template.Template, appVersion string) {
	RegisterPublicRoutes(mux, templ, appVersion)
	RegisterAuthRoutes(mux, templ, appVersion)
	RegisterWorkspaceRoutes(mux, templ, appVersion)
	RegisterChannelRoutes(mux, templ, appVersion)
	RegisterCompanyRoutes(mux, templ, appVersion)
	RegisterNotificationRoutes(mux, templ, appVersion)
	RegisterSettingsRoutes(mux, templ, appVersion)
	RegisterMessageRoutes(mux, templ, appVersion)
}
