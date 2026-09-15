package main

import (
	"net/http/httptest"
	"testing"
)

func TestIsAllowedOrigin(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "https://app.example.com")

	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{
			name: "allows non browser client without origin",
			host: "localhost:8081",
			want: true,
		},
		{
			name:   "allows same hostname on different port",
			host:   "localhost:8081",
			origin: "http://localhost",
			want:   true,
		},
		{
			name:   "allows explicitly configured origin",
			host:   "ws.example.com",
			origin: "https://app.example.com",
			want:   true,
		},
		{
			name:   "rejects untrusted origin",
			host:   "ws.example.com",
			origin: "https://evil.example.com",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://"+tt.host+"/ws/notifications", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if got := isAllowedOrigin(req); got != tt.want {
				t.Fatalf("isAllowedOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllowedOriginUsesForwardedHost(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "")
	req := httptest.NewRequest("GET", "http://websocket:8080/ws/notifications", nil)
	req.Header.Set("Origin", "https://messenger.example.com")
	req.Header.Set("X-Forwarded-Host", "messenger.example.com")

	if !isAllowedOrigin(req) {
		t.Fatal("expected forwarded public host to be accepted")
	}
}
