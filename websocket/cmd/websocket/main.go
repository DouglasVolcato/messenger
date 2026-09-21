package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	grpcapi "github.com/douglasvolcato/messager-architecture-challenge/internal/grpc"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultPort     = "8080"
	writeWait       = 5 * time.Second
	pongWait        = 30 * time.Second
	pingPeriod      = (pongWait * 9) / 10
	sendBuffer      = 64
	redisTTL        = 45 * time.Second
	defaultGRPCPort = "9090"
	registryTTL     = 90 * time.Second
)

type socketServer struct {
	mu      sync.Mutex
	clients map[string]*Client
}

type grpcDeliveryServer struct {
	grpcapi.UnimplementedWebSocketDeliveryServer
	socketServer *socketServer
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

	wsServer := &socketServer{clients: make(map[string]*Client)}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     isAllowedOrigin,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	grpcPort := strings.TrimSpace(os.Getenv("WEBSOCKET_GRPC_PORT"))
	if grpcPort == "" {
		grpcPort = defaultGRPCPort
	}
	grpcListener, err := net.Listen("tcp4", ":"+grpcPort)
	if err != nil {
		log.Fatalf("start gRPC listener: %v", err)
	}
	grpcServer := grpc.NewServer()
	grpcapi.RegisterWebSocketDeliveryServer(grpcServer, &grpcDeliveryServer{socketServer: wsServer})

	grpcAddress, err := privateAddress(grpcPort)
	if err != nil {
		log.Fatalf("resolve gRPC address: %v", err)
	}
	if err := registerGRPCEndpoint(shutdownCtx, serverID, grpcAddress); err != nil {
		log.Fatalf("register gRPC endpoint: %v", err)
	}
	defer func() {
		_ = cache.RDB.Del(context.Background(), grpcRegistryKey(serverID)).Err()
	}()
	go refreshGRPCEndpoint(shutdownCtx, serverID, grpcAddress)
	go func() {
		log.Printf("WebSocket server %s gRPC listening on %s", serverID, grpcAddress)
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

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
	grpcServer.GracefulStop()
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
	s.clients[c.connectionID] = c
}

func (s *socketServer) remove(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c.connectionID)
}

func (s *socketServer) closeAll() {
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	for _, client := range clients {
		client.close()
	}
}

func (s *socketServer) deliver(userID string, connectionIDs []string, payload []byte) {
	s.mu.Lock()
	clients := make([]*Client, 0, len(connectionIDs))
	for _, connectionID := range connectionIDs {
		client, ok := s.clients[connectionID]
		if ok && client.userID == userID {
			clients = append(clients, client)
		}
	}
	s.mu.Unlock()

	for _, client := range clients {
		metrics.IncNotificationAttempt()
		client.SendNotification(payload)
	}
}

func (s *grpcDeliveryServer) Deliver(_ context.Context, request *grpcapi.DeliveryRequest) (*empty.Empty, error) {
	if request.GetUserId() == "" || len(request.GetConnectionIds()) == 0 || request.GetNotification() == nil {
		return nil, status.Error(codes.InvalidArgument, "user_id, connection_ids and notification are required")
	}

	payload, err := json.Marshal(request.GetNotification())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal notification: %v", err)
	}
	s.socketServer.deliver(request.GetUserId(), request.GetConnectionIds(), payload)
	return &empty.Empty{}, nil
}

func grpcRegistryKey(serverID string) string {
	return "websocket:server:" + serverID + ":grpc"
}

func registerGRPCEndpoint(ctx context.Context, serverID, address string) error {
	return cache.RDB.Set(ctx, grpcRegistryKey(serverID), address, registryTTL).Err()
}

func refreshGRPCEndpoint(ctx context.Context, serverID, address string) {
	ticker := time.NewTicker(registryTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := registerGRPCEndpoint(ctx, serverID, address); err != nil {
				log.Printf("refresh gRPC endpoint for WebSocket server %s: %v", serverID, err)
			}
		}
	}
}

func privateAddress(port string) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				return net.JoinHostPort(ip.String(), port), nil
			}
		}
	}
	return "", errors.New("no private IPv4 address found")
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		c.server.remove(c)
		metrics.ConnectionClosed()

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
		metrics.IncSlowClient()
		log.Printf("[SLOW CLIENT] dropping connection for user %s", c.userID)
		c.close()
	}
}
