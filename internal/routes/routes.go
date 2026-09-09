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
	Success    string
	BackURL    string

	User        models.User
	Company     models.Company
	CompanyRole string
	CanManage   bool
	DirectUser  models.User

	UnreadNotifications int64

	Companies     []CompanyView
	Chats         []ChatView
	CurrentChat   ChatView
	Members       []MemberView
	Member        MemberView
	Messages      []MessageView
	Notifications []models.UserNotification

	Page     int
	PrevPage int
	NextPage int
	Limit    int
	Total    int64
	HasPrev  bool
	HasNext  bool

	ClientMessageID string
	Identifier      string
	Name            string
	Username        string
	Email           string
}

type CompanyView struct {
	ID        string
	Name      string
	Status    string
	Role      string
	CanManage bool
}

type ChatView struct {
	ID          string
	CompanyID   string
	Name        string
	Subscribed  bool
	MemberCount int64
}

type ReactionView struct {
	Reaction string
	Count    int64
	Reacted  bool
}

type MessageView struct {
	ID        string
	UserID    string
	UserName  string
	Content   string
	Direct    bool
	CreatedAt time.Time
	EditedAt  *time.Time
	DeletedAt *time.Time
	Reactions []ReactionView
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
	RegisterCompanyRoutes(mux, templ, appVersion)
	RegisterNotificationRoutes(mux, templ, appVersion)
	RegisterSettingsRoutes(mux, templ, appVersion)
	RegisterMessageRoutes(mux, templ, appVersion)
}
