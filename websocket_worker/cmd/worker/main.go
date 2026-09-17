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
	"github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/subosito/gotenv"
)

const reconnectDelay = 5 * time.Second

type notificationEvent struct {
	ID      string
	UserID  string
	Type    string
	Title   *string
	Content string
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
		log.Printf("discarding invalid %s priority event message_id=%q: %v", priorityQueue.Priority, delivery.MessageId, err)
		return nil
	}
	if event.UserID == "" {
		log.Printf("discarding %s priority event message_id=%q without user_id", priorityQueue.Priority, delivery.MessageId)
		return nil
	}

	sessions, err := loadActiveSessions(ctx, event.UserID)
	if err != nil {
		return fmt.Errorf("read WebSocket sessions for user %q: %w", event.UserID, err)
	}

	log.Printf(
		"WebSocket event received priority=%s queue=%s message_id=%q event_id=%q user_id=%q type=%q active_sessions=%d sessions=%v; delivery disabled",
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
