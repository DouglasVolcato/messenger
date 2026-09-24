package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	rabbitmq "github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/gorilla/websocket"
)

const (
	defaultPort          = "8080"
	writeWait            = 5 * time.Second
	pongWait             = 30 * time.Second
	pingPeriod           = (pongWait * 9) / 10
	sendBuffer           = 64
	sessionTTL           = 45 * time.Second
	sessionRegistryTTL   = 7 * 24 * time.Hour
	replicaRegistryTTL   = 90 * time.Second
	rabbitReconnectDelay = 5 * time.Second
)

type socketServer struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

type Client struct {
	userID       string
	connectionID string
	replicaID    string
	conn         *websocket.Conn
	send         chan []byte
	done         chan struct{}
	closeOnce    sync.Once
	server       *socketServer
}

func main() {
	replicaID := strings.TrimSpace(os.Getenv("WEBSOCKET_REPLICA_ID"))
	if replicaID == "" {
		generatedID, err := utils.GenerateUUID()
		if err != nil {
			log.Fatalf("generate WebSocket replica ID: %v", err)
		}
		replicaID = generatedID
	}

	if err := cache.InitRedisClient(); err != nil {
		log.Fatalf("initialize WebSocket Redis registry: %v", err)
	}
	defer func() {
		if err := cache.CloseRedisClient(); err != nil {
			log.Printf("close WebSocket Redis client: %v", err)
		}
	}()

	wsServer := &socketServer{clients: make(map[string]*Client)}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     isAllowedOrigin,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var deliveryReady atomic.Bool
	go consumeReplicaDeliveries(shutdownCtx, replicaID, wsServer, &deliveryReady)

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("/livez", liveHandler)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readinessHandler(w, r, &deliveryReady)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		readinessHandler(w, r, &deliveryReady)
	})
	mux.HandleFunc("/ws/notifications", func(w http.ResponseWriter, r *http.Request) {
		if !deliveryReady.Load() {
			http.Error(w, "WebSocket delivery route unavailable", http.StatusServiceUnavailable)
			return
		}

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

		connectionID, err := utils.GenerateUUID()
		if err != nil {
			log.Printf("generate WebSocket connection ID: %v", err)
			_ = conn.Close()
			return
		}

		client := &Client{
			userID:       user.ID,
			connectionID: connectionID,
			replicaID:    replicaID,
			conn:         conn,
			send:         make(chan []byte, sendBuffer),
			done:         make(chan struct{}),
			server:       wsServer,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err = registerSession(ctx, client)
		cancel()
		if err != nil {
			log.Printf("register WebSocket session %s: %v", connectionID, err)
			_ = conn.Close()
			return
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
		log.Printf("WebSocket replica %s listening on :%s", replicaID, port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			log.Fatalf("WebSocket server failed: %v", err)
		}
	case <-shutdownCtx.Done():
		log.Printf("shutting down WebSocket replica %s", replicaID)
	}

	deliveryReady.Store(false)
	_ = cache.RDB.Del(context.Background(), replicaRegistryKey(replicaID)).Err()

	gracefulCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(gracefulCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	wsServer.closeAll()
}

func consumeReplicaDeliveries(ctx context.Context, replicaID string, wsServer *socketServer, ready *atomic.Bool) {
	for ctx.Err() == nil {
		consumer, err := rabbitmq.NewConsumer(os.Getenv("RABBITMQ_URL"), replicaID)
		if err != nil {
			ready.Store(false)
			metrics.IncRabbitReconnect()
			log.Printf("RabbitMQ unavailable for WebSocket replica %s: %v; retrying in %s", replicaID, err, rabbitReconnectDelay)
			if !wait(ctx, rabbitReconnectDelay) {
				return
			}
			continue
		}

		consumerCtx, cancel := context.WithCancel(ctx)
		if err := registerReplica(consumerCtx, replicaID, consumer.QueueName()); err != nil {
			cancel()
			_ = consumer.Close()
			ready.Store(false)
			log.Printf("register WebSocket replica %s in Redis: %v", replicaID, err)
			if !wait(ctx, rabbitReconnectDelay) {
				return
			}
			continue
		}

		ready.Store(true)
		go refreshReplicaPresence(consumerCtx, replicaID, consumer.QueueName())
		log.Printf("WebSocket replica %s consuming RabbitMQ queue %s", replicaID, consumer.QueueName())

		err = consumer.Run(consumerCtx, func(_ context.Context, command rabbitmq.Delivery) error {
			payload, marshalErr := json.Marshal(command.Notification)
			if marshalErr != nil {
				metrics.IncDeliveryFailure()
				return marshalErr
			}
			wsServer.deliver(command.UserID, command.ConnectionIDs, payload)
			metrics.IncDeliveryCommand()
			return nil
		})

		ready.Store(false)
		cancel()
		_ = cache.RDB.Del(context.Background(), replicaRegistryKey(replicaID)).Err()
		if closeErr := consumer.Close(); closeErr != nil && ctx.Err() == nil {
			log.Printf("close WebSocket RabbitMQ consumer: %v", closeErr)
		}
		if ctx.Err() != nil {
			return
		}

		metrics.IncRabbitReconnect()
		log.Printf("WebSocket replica %s RabbitMQ consumer stopped: %v; reconnecting in %s", replicaID, err, rabbitReconnectDelay)
		if !wait(ctx, rabbitReconnectDelay) {
			return
		}
	}
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func readinessHandler(w http.ResponseWriter, r *http.Request, deliveryReady *atomic.Bool) {
	if !deliveryReady.Load() {
		http.Error(w, "rabbitmq delivery route unavailable", http.StatusServiceUnavailable)
		return
	}

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

func registerReplica(ctx context.Context, replicaID, queueName string) error {
	return cache.RDB.Set(ctx, replicaRegistryKey(replicaID), queueName, replicaRegistryTTL).Err()
}

func refreshReplicaPresence(ctx context.Context, replicaID, queueName string) {
	ticker := time.NewTicker(replicaRegistryTTL / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := registerReplica(refreshCtx, replicaID, queueName)
			cancel()
			if err != nil {
				log.Printf("refresh WebSocket replica registry %s: %v", replicaID, err)
			}
		}
	}
}

func replicaRegistryKey(replicaID string) string {
	return "websocket:replica:" + replicaID
}

func sessionRegistryKey(userID string) string {
	return "user:sessions:" + userID
}

func registerSession(ctx context.Context, client *Client) error {
	key := sessionRegistryKey(client.userID)
	value := fmt.Sprintf("%s:%d", client.replicaID, time.Now().Add(sessionTTL).Unix())

	if err := cache.RDB.HSet(ctx, key, client.connectionID, value).Err(); err != nil {
		return err
	}
	if err := cache.RDB.Expire(ctx, key, sessionRegistryTTL).Err(); err != nil {
		_ = cache.RDB.HDel(context.Background(), key, client.connectionID).Err()
		return err
	}
	return nil
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

func (s *socketServer) add(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.connectionID] = client
}

func (s *socketServer) remove(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, client.connectionID)
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

func (s *socketServer) deliver(userID string, connectionIDs []string, payload []byte) int {
	s.mu.RLock()
	clients := make([]*Client, 0, len(connectionIDs))
	for _, connectionID := range connectionIDs {
		client, ok := s.clients[connectionID]
		if ok && client.userID == userID {
			clients = append(clients, client)
		}
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
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.server.remove(c)
		metrics.ConnectionClosed()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cache.RDB.HDel(ctx, sessionRegistryKey(c.userID), c.connectionID).Err(); err != nil {
			log.Printf("remove WebSocket session %s: %v", c.connectionID, err)
		}
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
		case message := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := registerSession(ctx, c)
			cancel()
			if err != nil {
				log.Printf("refresh WebSocket session %s: %v", c.connectionID, err)
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
