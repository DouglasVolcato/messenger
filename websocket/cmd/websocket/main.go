package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	rabbitmq "github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/gorilla/websocket"
)

const (
	defaultPort       = "8080"
	writeWait         = 5 * time.Second
	pongWait          = 30 * time.Second
	pingPeriod        = (pongWait * 9) / 10
	sendBuffer        = 64
	rabbitReconnect   = 5 * time.Second
)

type socketServer struct {
	mu      sync.RWMutex
	clients map[string]*Client
	byUser  map[string]map[string]*Client
}

type Client struct {
	userID       string
	connectionID string
	conn         *websocket.Conn
	send         chan []byte
	done         chan struct{}
	closeOnce    sync.Once
	server       *socketServer
}

type outboundNotification struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	ActionURL string `json:"action_url,omitempty"`
}

func main() {
	wsServer := &socketServer{
		clients: make(map[string]*Client),
		byUser:  make(map[string]map[string]*Client),
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     isAllowedOrigin,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go consumeRealtimeNotifications(shutdownCtx, wsServer)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("/ws/notifications", func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			metrics.IncAuthFailure()
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			metrics.IncUpgradeFailure()
			log.Printf("websocket upgrade error: %v", err)
			return
		}

		connID, err := utils.GenerateUUID()
		if err != nil {
			log.Printf("failed to generate connection ID: %v", err)
			_ = conn.Close()
			return
		}

		client := &Client{
			userID:       user.ID,
			connectionID: connID,
			conn:         conn,
			send:         make(chan []byte, sendBuffer),
			done:         make(chan struct{}),
			server:       wsServer,
		}

		wsServer.add(client)
		metrics.ConnectionOpened()
		go client.writePump()
		go client.readPump()
	})

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("WebSocket server listening on :%s", port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			log.Fatalf("websocket server failed: %v", err)
		}
	case <-shutdownCtx.Done():
		log.Printf("shutting down WebSocket server")
	}

	gracefulCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(gracefulCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	wsServer.closeAll()
}

func consumeRealtimeNotifications(ctx context.Context, wsServer *socketServer) {
	for ctx.Err() == nil {
		consumer, err := rabbitmq.NewConsumer(os.Getenv("RABBITMQ_URL"))
		if err != nil {
			metrics.IncRabbitReconnect()
			log.Printf("RabbitMQ unavailable for WebSocket replica: %v; retrying in %s", err, rabbitReconnect)
			if !wait(ctx, rabbitReconnect) {
				return
			}
			continue
		}

		log.Printf("WebSocket replica consuming RabbitMQ fanout queue %s", consumer.QueueName())
		err = consumer.Run(ctx, func(_ context.Context, event rabbitmq.Event) error {
			return deliverRealtimeEvent(wsServer, event)
		})
		if closeErr := consumer.Close(); closeErr != nil && ctx.Err() == nil {
			log.Printf("close WebSocket RabbitMQ consumer: %v", closeErr)
		}
		if ctx.Err() != nil {
			return
		}

		metrics.IncRabbitReconnect()
		log.Printf("WebSocket RabbitMQ consumer stopped: %v; reconnecting in %s", err, rabbitReconnect)
		if !wait(ctx, rabbitReconnect) {
			return
		}
	}
}

func deliverRealtimeEvent(wsServer *socketServer, event rabbitmq.Event) error {
	payload, err := json.Marshal(outboundNotification{
		ID:        event.ID,
		Type:      event.Type,
		Title:     stringValue(event.Title),
		Content:   event.Content,
		ActionURL: stringValue(event.ActionURL),
	})
	if err != nil {
		metrics.IncRealtimeEventFailure()
		return err
	}

	delivered := wsServer.deliver(event.UserID, payload)
	metrics.IncRealtimeEvent(delivered)
	return nil
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func isAllowedOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return false
	}

	requestHost := r.Host
	if forwardedHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwardedHost != "" {
		requestHost = forwardedHost
	}

	requestURL := &url.URL{Host: requestHost}
	if strings.EqualFold(parsedOrigin.Hostname(), requestURL.Hostname()) {
		return true
	}

	for _, allowedOrigin := range strings.Split(os.Getenv("WEBSOCKET_ALLOWED_ORIGINS"), ",") {
		allowedOrigin = strings.TrimSpace(allowedOrigin)
		if allowedOrigin == "" {
			continue
		}
		parsedAllowed, err := url.Parse(allowedOrigin)
		if err != nil || parsedAllowed.Scheme == "" || parsedAllowed.Host == "" {
			continue
		}
		if strings.EqualFold(parsedOrigin.Scheme, parsedAllowed.Scheme) && strings.EqualFold(parsedOrigin.Host, parsedAllowed.Host) {
			return true
		}
	}

	return false
}

func (s *socketServer) add(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[c.connectionID] = c
	if s.byUser[c.userID] == nil {
		s.byUser[c.userID] = make(map[string]*Client)
	}
	s.byUser[c.userID][c.connectionID] = c
}

func (s *socketServer) remove(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, c.connectionID)
	userConnections := s.byUser[c.userID]
	delete(userConnections, c.connectionID)
	if len(userConnections) == 0 {
		delete(s.byUser, c.userID)
	}
}

func (s *socketServer) closeAll() {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		client.close()
	}
}

func (s *socketServer) deliver(userID string, payload []byte) int {
	s.mu.RLock()
	userConnections := s.byUser[userID]
	clients := make([]*Client, 0, len(userConnections))
	for _, client := range userConnections {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		metrics.IncNotificationAttempt()
		client.SendNotification(payload)
	}
	return len(clients)
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		c.server.remove(c)
		metrics.ConnectionClosed()
	})
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case <-c.done:
			return
		case msg := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
	defer c.close()

	c.conn.SetReadLimit(128)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) SendNotification(notification []byte) {
	select {
	case <-c.done:
		return
	case c.send <- notification:
	default:
		metrics.IncSlowClient()
		log.Printf("[SLOW CLIENT] dropping connection for user %s", c.userID)
		c.close()
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
