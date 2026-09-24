package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/cache"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/subosito/gotenv"
)

const reconnectDelay = 5 * time.Second

type notificationEvent struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Type      string  `json:"type"`
	Title     *string `json:"title"`
	Content   string  `json:"content"`
	ActionURL *string `json:"action_url"`
}

type websocketSession struct {
	ConnectionID string
	ReplicaID    string
	ExpiresAt    int64
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadEnv()

	metricsPort := strings.TrimSpace(os.Getenv("METRICS_PORT"))
	if metricsPort == "" {
		metricsPort = "9090"
	}
	go metrics.Serve(ctx, ":"+metricsPort)

	if err := connectRedis(ctx); err != nil {
		log.Printf("WebSocket worker stopped while connecting to Redis: %v", err)
		return
	}
	defer func() {
		if err := cache.CloseRedisClient(); err != nil {
			log.Printf("close Redis client: %v", err)
		}
	}()

	rabbitURL := os.Getenv("RABBITMQ_URL")
	for ctx.Err() == nil {
		consumer, err := rabbitmq.NewConsumer(rabbitURL)
		if err != nil {
			metrics.IncRabbitReconnect()
			log.Printf("RabbitMQ notification consumer unavailable: %v; retrying in %s", err, reconnectDelay)
			wait(ctx, reconnectDelay)
			continue
		}

		router, err := rabbitmq.NewRouter(rabbitURL)
		if err != nil {
			_ = consumer.Close()
			metrics.IncRabbitReconnect()
			log.Printf("RabbitMQ WebSocket delivery router unavailable: %v; retrying in %s", err, reconnectDelay)
			wait(ctx, reconnectDelay)
			continue
		}

		metrics.SetReady(true)
		log.Printf("WebSocket worker connected to RabbitMQ")
		err = consumer.Run(ctx, func(deliveryCtx context.Context, priorityQueue rabbitmq.PriorityQueue, delivery amqp.Delivery) error {
			return handleDelivery(deliveryCtx, priorityQueue, delivery, router)
		})

		metrics.SetReady(false)
		if closeErr := router.Close(); closeErr != nil && ctx.Err() == nil {
			log.Printf("close RabbitMQ WebSocket delivery router: %v", closeErr)
		}
		if closeErr := consumer.Close(); closeErr != nil && ctx.Err() == nil {
			log.Printf("close RabbitMQ notification consumer: %v", closeErr)
		}

		if err != nil && ctx.Err() == nil {
			metrics.IncRabbitReconnect()
			log.Printf("WebSocket worker RabbitMQ connection stopped: %v; reconnecting in %s", err, reconnectDelay)
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

func handleDelivery(
	ctx context.Context,
	priorityQueue rabbitmq.PriorityQueue,
	delivery amqp.Delivery,
	router *rabbitmq.Router,
) (err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveProcessing(time.Since(started))
		if err != nil {
			metrics.IncFailed(priorityQueue.Priority)
		} else {
			metrics.IncProcessed(priorityQueue.Priority)
		}
	}()

	var event notificationEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		return fmt.Errorf("decode %s priority event message_id=%q: %w", priorityQueue.Priority, delivery.MessageId, err)
	}
	if event.ID == "" || event.UserID == "" {
		return fmt.Errorf("%s priority event message_id=%q is missing id or user_id", priorityQueue.Priority, delivery.MessageId)
	}

	sessions, err := loadActiveSessions(ctx, event.UserID)
	if err != nil {
		return fmt.Errorf("read WebSocket sessions for user %q: %w", event.UserID, err)
	}
	metrics.AddSessions(len(sessions))

	if err := deliverToWebSocketReplicas(ctx, event, sessions, router); err != nil {
		return err
	}

	log.Printf(
		"WebSocket event routed priority=%s queue=%s message_id=%q event_id=%q user_id=%q active_sessions=%d",
		priorityQueue.Priority,
		priorityQueue.Name,
		delivery.MessageId,
		event.ID,
		event.UserID,
		len(sessions),
	)
	return nil
}

func deliverToWebSocketReplicas(
	ctx context.Context,
	event notificationEvent,
	sessions []websocketSession,
	router *rabbitmq.Router,
) error {
	connectionIDsByReplica := make(map[string][]string)
	for _, session := range sessions {
		connectionIDsByReplica[session.ReplicaID] = append(connectionIDsByReplica[session.ReplicaID], session.ConnectionID)
	}

	for replicaID, connectionIDs := range connectionIDsByReplica {
		active, err := replicaIsActive(ctx, replicaID)
		if err != nil {
			return fmt.Errorf("check WebSocket replica %q: %w", replicaID, err)
		}
		if !active {
			metrics.IncStaleReplicaRoute()
			if err := removeSessions(ctx, event.UserID, connectionIDs); err != nil {
				log.Printf("remove stale sessions for WebSocket replica %s: %v", replicaID, err)
			}
			continue
		}

		command := rabbitmq.Delivery{
			UserID:        event.UserID,
			ConnectionIDs: connectionIDs,
			Notification: rabbitmq.Notification{
				ID:        event.ID,
				Type:      event.Type,
				Title:     event.Title,
				Content:   event.Content,
				ActionURL: event.ActionURL,
			},
		}
		if err := router.Publish(ctx, replicaID, command); err != nil {
			if errors.Is(err, rabbitmq.ErrReplicaUnavailable) {
				metrics.IncStaleReplicaRoute()
				if cleanupErr := removeSessions(ctx, event.UserID, connectionIDs); cleanupErr != nil {
					log.Printf("remove sessions for unavailable WebSocket replica %s: %v", replicaID, cleanupErr)
				}
				continue
			}
			metrics.IncDeliveryFailure()
			return err
		}
		metrics.IncDeliveryRoute()
	}

	return nil
}

func loadActiveSessions(ctx context.Context, userID string) ([]websocketSession, error) {
	key := sessionRegistryKey(userID)
	values, err := cache.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	sessions := make([]websocketSession, 0, len(values))
	expired := make([]string, 0)
	for connectionID, value := range values {
		replicaID, expiresAt, ok := strings.Cut(value, ":")
		if !ok || replicaID == "" {
			expired = append(expired, connectionID)
			continue
		}

		expiresAtUnix, err := strconv.ParseInt(expiresAt, 10, 64)
		if err != nil || expiresAtUnix <= now {
			expired = append(expired, connectionID)
			continue
		}
		sessions = append(sessions, websocketSession{
			ConnectionID: connectionID,
			ReplicaID:    replicaID,
			ExpiresAt:    expiresAtUnix,
		})
	}

	if len(expired) > 0 {
		if err := cache.RDB.HDel(ctx, key, expired...).Err(); err != nil {
			log.Printf("remove expired WebSocket sessions for user %s: %v", userID, err)
		}
	}

	return sessions, nil
}

func replicaIsActive(ctx context.Context, replicaID string) (bool, error) {
	count, err := cache.RDB.Exists(ctx, replicaRegistryKey(replicaID)).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func removeSessions(ctx context.Context, userID string, connectionIDs []string) error {
	if len(connectionIDs) == 0 {
		return nil
	}
	return cache.RDB.HDel(ctx, sessionRegistryKey(userID), connectionIDs...).Err()
}

func replicaRegistryKey(replicaID string) string {
	return "websocket:replica:" + replicaID
}

func sessionRegistryKey(userID string) string {
	return "user:sessions:" + userID
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
