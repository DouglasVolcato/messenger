package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/cache"
	grpcapi "github.com/douglasvolcato/messager-architecture-challenge/internal/grpc"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/subosito/gotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const reconnectDelay = 5 * time.Second

type notificationEvent struct {
	ID        string
	UserID    string
	Type      string
	Title     *string
	Content   string
	ActionURL *string
}

type websocketSession struct {
	ConnectionID string
	ServerID     string
	ExpiresAt    int64
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadEnv()
	if err := connectRedis(ctx); err != nil {
		log.Printf("WebSocket worker stopped while connecting to Redis: %v", err)
		return
	}
	defer func() {
		if err := cache.CloseRedisClient(); err != nil {
			log.Printf("close Redis client: %v", err)
		}
	}()

	for ctx.Err() == nil {
		consumer, err := rabbitmq.NewConsumer(os.Getenv("RABBITMQ_URL"))
		if err != nil {
			log.Printf("RabbitMQ unavailable: %v; retrying in %s", err, reconnectDelay)
			wait(ctx, reconnectDelay)
			continue
		}

		log.Printf("WebSocket worker connected to RabbitMQ")
		err = consumer.Run(ctx, handleDelivery)
		if closeErr := consumer.Close(); closeErr != nil && ctx.Err() == nil {
			log.Printf("close RabbitMQ connection: %v", closeErr)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("RabbitMQ consumer stopped: %v; reconnecting in %s", err, reconnectDelay)
			wait(ctx, reconnectDelay)
		}
	}
}

func loadEnv() {
	if err := gotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			log.Fatalf("load environment: %v", err)
		}
		if parentErr := gotenv.Load("../.env"); parentErr != nil && !os.IsNotExist(parentErr) {
			log.Fatalf("load parent environment: %v", parentErr)
		}
	}
}

func connectRedis(ctx context.Context) error {
	for {
		if err := cache.InitRedisClient(); err == nil {
			log.Printf("WebSocket worker connected to Redis")
			return nil
		} else {
			log.Printf("Redis unavailable: %v; retrying in %s", err, reconnectDelay)
		}

		if !wait(ctx, reconnectDelay) {
			return ctx.Err()
		}
	}
}

func handleDelivery(ctx context.Context, priorityQueue rabbitmq.PriorityQueue, delivery amqp.Delivery) error {
	var event notificationEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		return fmt.Errorf("decode %s priority event message_id=%q: %w", priorityQueue.Priority, delivery.MessageId, err)
	}
	if event.UserID == "" {
		return fmt.Errorf("%s priority event message_id=%q is missing user_id", priorityQueue.Priority, delivery.MessageId)
	}

	sessions, err := loadActiveSessions(ctx, event.UserID)
	if err != nil {
		return fmt.Errorf("read WebSocket sessions for user %q: %w", event.UserID, err)
	}

	if err := deliverToWebSocketServers(ctx, event, sessions); err != nil {
		return err
	}

	log.Printf(
		"WebSocket event delivered priority=%s queue=%s message_id=%q event_id=%q user_id=%q type=%q active_sessions=%d sessions=%v",
		priorityQueue.Priority,
		priorityQueue.Name,
		delivery.MessageId,
		event.ID,
		event.UserID,
		event.Type,
		len(sessions),
		sessions,
	)
	return nil
}

func deliverToWebSocketServers(ctx context.Context, event notificationEvent, sessions []websocketSession) error {
	connectionIDsByServer := make(map[string][]string)
	for _, session := range sessions {
		connectionIDsByServer[session.ServerID] = append(connectionIDsByServer[session.ServerID], session.ConnectionID)
	}

	for serverID, connectionIDs := range connectionIDsByServer {
		address, err := cache.RDB.Get(ctx, grpcRegistryKey(serverID)).Result()
		if err != nil {
			return fmt.Errorf("get gRPC endpoint for WebSocket server %q: %w", serverID, err)
		}

		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		connection, err := grpc.DialContext(callCtx, address, grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			cancel()
			return fmt.Errorf("connect to WebSocket server %q gRPC endpoint %q: %w", serverID, address, err)
		}

		_, err = grpcapi.NewWebSocketDeliveryClient(connection).Deliver(callCtx, &grpcapi.DeliveryRequest{
			UserId:        event.UserID,
			ConnectionIds: connectionIDs,
			Notification: &grpcapi.Notification{
				Id:        event.ID,
				Type:      event.Type,
				Title:     stringValue(event.Title),
				Content:   event.Content,
				ActionUrl: stringValue(event.ActionURL),
			},
		})
		closeErr := connection.Close()
		cancel()
		if err != nil {
			return fmt.Errorf("deliver notification to WebSocket server %q: %w", serverID, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close gRPC connection to WebSocket server %q: %w", serverID, closeErr)
		}
	}

	return nil
}

func grpcRegistryKey(serverID string) string {
	return "websocket:server:" + serverID + ":grpc"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func loadActiveSessions(ctx context.Context, userID string) ([]websocketSession, error) {
	values, err := cache.RDB.HGetAll(ctx, "user:sessions:"+userID).Result()
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	sessions := make([]websocketSession, 0, len(values))
	for connectionID, value := range values {
		serverID, expiresAt, ok := strings.Cut(value, ":")
		if !ok {
			continue
		}
		expiresAtUnix, err := strconv.ParseInt(expiresAt, 10, 64)
		if err != nil || expiresAtUnix <= now {
			continue
		}
		sessions = append(sessions, websocketSession{
			ConnectionID: connectionID,
			ServerID:     serverID,
			ExpiresAt:    expiresAtUnix,
		})
	}

	return sessions, nil
}

func wait(ctx context.Context, delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return true
	case <-ctx.Done():
		return false
	}
}
