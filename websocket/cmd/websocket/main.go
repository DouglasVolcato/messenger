package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/gorilla/websocket"
)

const (
	defaultPort = "8080"
	writeWait   = 5 * time.Second
	pongWait    = 30 * time.Second
	pingPeriod  = (pongWait * 9) / 10
	sendBuffer  = 64
	redisTTL    = 45 * time.Second
)

type socketServer struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
}

type Client struct {
	userID       string
	connectionID string
	serverID     string
	conn         *websocket.Conn
	send         chan []byte
	done         chan struct{}
	closeOnce    sync.Once
	server       *socketServer
}

func main() {
	serverID, err := utils.GenerateUUID()
	if err != nil {
		log.Fatalf("failed to generate server ID: %v", err)
	}

	if err := cache.InitRedisClient(); err != nil {
		log.Fatalf("failed to initialize Redis client: %v", err)
	}
	defer func() {
		if err := cache.CloseRedisClient(); err != nil {
			log.Printf("failed to close Redis client: %v", err)
		}
	}()

	wsServer := &socketServer{clients: make(map[*Client]struct{})}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     isAllowedOrigin,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/ws/notifications", func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
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
			serverID:     serverID,
			conn:         conn,
			send:         make(chan []byte, sendBuffer),
			done:         make(chan struct{}),
			server:       wsServer,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		redisKey := fmt.Sprintf("user:sessions:%s", user.ID)
		expiresAt := time.Now().Add(redisTTL).Unix()
		fieldValue := fmt.Sprintf("%s:%d", serverID, expiresAt)

		if err := cache.RDB.HSet(ctx, redisKey, connID, fieldValue).Err(); err != nil {
			log.Printf("failed to save session in Redis: %v", err)
			_ = conn.Close()
			return
		}
		if err := cache.RDB.Expire(ctx, redisKey, 7*24*time.Hour).Err(); err != nil {
			log.Printf("failed to set session registry expiration: %v", err)
			_ = cache.RDB.HDel(context.Background(), redisKey, connID).Err()
			_ = conn.Close()
			return
		}

		wsServer.add(client)
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

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("WebSocket server %s listening on :%s", serverID, port)
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
		log.Printf("shutting down WebSocket server %s", serverID)
	}

	gracefulCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(gracefulCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	wsServer.closeAll()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := cache.RDB.Ping(ctx).Err(); err != nil {
		http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
		return
	}
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
	s.clients[c] = struct{}{}
}

func (s *socketServer) remove(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

func (s *socketServer) closeAll() {
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	for _, client := range clients {
		client.close()
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		c.server.remove(c)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		redisKey := fmt.Sprintf("user:sessions:%s", c.userID)
		if err := cache.RDB.HDel(ctx, redisKey, c.connectionID).Err(); err != nil {
			log.Printf("failed to remove WebSocket session %s: %v", c.connectionID, err)
		}
	})
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	redisKey := fmt.Sprintf("user:sessions:%s", c.userID)

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

			expiresAt := time.Now().Add(redisTTL).Unix()
			fieldValue := fmt.Sprintf("%s:%d", c.serverID, expiresAt)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := cache.RDB.HSet(ctx, redisKey, c.connectionID, fieldValue).Err()
			cancel()
			if err != nil {
				log.Printf("failed to refresh WebSocket session %s: %v", c.connectionID, err)
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
		log.Printf("[SLOW CLIENT] dropping connection for user %s", c.userID)
		c.close()
	}
}
