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
	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/subosito/gotenv"
)

const (
	reconnectDelay      = 5 * time.Second
	chatMembersCacheTTL = 5 * time.Minute
)

type notificationEvent struct {
	ID           string  `json:"id"`
	UserID       string  `json:"user_id,omitempty"`
	ChatID       string  `json:"chat_id,omitempty"`
	CompanyID    string  `json:"company_id,omitempty"`
	SenderUserID string  `json:"sender_user_id,omitempty"`
	MessageID    string  `json:"message_id,omitempty"`
	Type         string  `json:"type"`
	Title        *string `json:"title"`
	Content      string  `json:"content"`
	ActionURL    *string `json:"action_url"`
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

	if err := connectDatabase(ctx); err != nil {
		log.Printf("WebSocket worker stopped while connecting to PostgreSQL: %v", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close PostgreSQL: %v", err)
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

func connectDatabase(ctx context.Context) error {
	for {
		if err := db.Init(); err == nil {
			log.Printf("WebSocket worker connected to PostgreSQL")
			return nil
		} else {
			log.Printf("PostgreSQL unavailable: %v; retrying in %s", err, reconnectDelay)
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
	if event.ID == "" {
		return fmt.Errorf("%s priority event message_id=%q is missing id", priorityQueue.Priority, delivery.MessageId)
	}

	if event.Type == "CHAT_MESSAGE" && event.ChatID != "" {
		return handleChatMessageDelivery(ctx, priorityQueue, delivery.MessageId, event, router)
	}
	if event.UserID == "" {
		return fmt.Errorf("%s priority event message_id=%q is missing user_id", priorityQueue.Priority, delivery.MessageId)
	}

	sessions, err := loadActiveSessions(ctx, event.UserID)
	if err != nil {
		return fmt.Errorf("read WebSocket sessions for user %q: %w", event.UserID, err)
	}
	metrics.AddSessions(len(sessions))

	targetsByReplica := make(map[string][]rabbitmq.DeliveryTarget)
	for _, session := range sessions {
		targets := targetsByReplica[session.ReplicaID]
		if len(targets) == 0 {
			targets = append(targets, rabbitmq.DeliveryTarget{UserID: event.UserID})
		}
		targets[0].ConnectionIDs = append(targets[0].ConnectionIDs, session.ConnectionID)
		targetsByReplica[session.ReplicaID] = targets
	}

	if err := deliverTargetsToReplicas(ctx, event, targetsByReplica, router); err != nil {
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

func handleChatMessageDelivery(
	ctx context.Context,
	priorityQueue rabbitmq.PriorityQueue,
	messageID string,
	event notificationEvent,
	router *rabbitmq.Router,
) error {
	members, err := loadChatMembers(ctx, event.ChatID)
	if err != nil {
		return fmt.Errorf("load chat members for chat %q: %w", event.ChatID, err)
	}

	recipients := make([]string, 0, len(members))
	for _, userID := range members {
		if userID == "" || userID == event.SenderUserID {
			continue
		}
		recipients = append(recipients, userID)
	}

	sessionsByUser, err := loadActiveSessionsForUsers(ctx, recipients)
	if err != nil {
		return fmt.Errorf("load active chat sessions for chat %q: %w", event.ChatID, err)
	}

	targetsByReplica := make(map[string][]rabbitmq.DeliveryTarget)
	activeSessions := 0
	for userID, sessions := range sessionsByUser {
		activeSessions += len(sessions)
		connectionIDsByReplica := make(map[string][]string)
		for _, session := range sessions {
			connectionIDsByReplica[session.ReplicaID] = append(connectionIDsByReplica[session.ReplicaID], session.ConnectionID)
		}
		for replicaID, connectionIDs := range connectionIDsByReplica {
			targetsByReplica[replicaID] = append(targetsByReplica[replicaID], rabbitmq.DeliveryTarget{
				UserID:        userID,
				ConnectionIDs: connectionIDs,
			})
		}
	}
	metrics.AddSessions(activeSessions)

	if err := deliverTargetsToReplicas(ctx, event, targetsByReplica, router); err != nil {
		return err
	}

	log.Printf(
		"Chat event routed priority=%s queue=%s message_id=%q event_id=%q chat_id=%q members=%d active_sessions=%d target_replicas=%d",
		priorityQueue.Priority,
		priorityQueue.Name,
		messageID,
		event.ID,
		event.ChatID,
		len(recipients),
		activeSessions,
		len(targetsByReplica),
	)
	return nil
}

func deliverTargetsToReplicas(
	ctx context.Context,
	event notificationEvent,
	targetsByReplica map[string][]rabbitmq.DeliveryTarget,
	router *rabbitmq.Router,
) error {
	for replicaID, targets := range targetsByReplica {
		active, err := replicaIsActive(ctx, replicaID)
		if err != nil {
			return fmt.Errorf("check WebSocket replica %q: %w", replicaID, err)
		}
		if !active {
			metrics.IncStaleReplicaRoute()
			removeTargetSessions(ctx, replicaID, targets)
			continue
		}

		command := rabbitmq.Delivery{
			Targets: targets,
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
				removeTargetSessions(ctx, replicaID, targets)
				continue
			}
			metrics.IncDeliveryFailure()
			return err
		}
		metrics.IncDeliveryRoute()
	}
	return nil
}

func loadChatMembers(ctx context.Context, chatID string) ([]string, error) {
	key := chatMembersCacheKey(chatID)
	data, err := cache.RDB.Get(ctx, key).Bytes()
	if err == nil {
		var members []string
		if unmarshalErr := json.Unmarshal(data, &members); unmarshalErr == nil {
			metrics.IncChatMemberCacheHit()
			return members, nil
		}
		_ = cache.RDB.Del(ctx, key).Err()
	} else if !errors.Is(err, redis.Nil) {
		return nil, err
	}

	metrics.IncChatMemberCacheMiss()
	rows, err := db.DB.QueryContext(ctx, `
		SELECT cu.user_id
		FROM chat_users cu
		JOIN chats ch
		  ON ch.id = cu.chat_id
		JOIN company_users company_member
		  ON company_member.user_id = cu.user_id
		 AND company_member.company_id = ch.company_id
		JOIN users u
		  ON u.id = cu.user_id
		 AND u.status = 'ACTIVE'
		WHERE cu.chat_id = $1
		ORDER BY cu.user_id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]string, 0)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		members = append(members, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(members)
	if err != nil {
		return nil, err
	}
	if err := cache.RDB.Set(ctx, key, encoded, chatMembersCacheTTL).Err(); err != nil {
		log.Printf("cache chat members chat_id=%s: %v", chatID, err)
	}
	return members, nil
}

func loadActiveSessionsForUsers(ctx context.Context, userIDs []string) (map[string][]websocketSession, error) {
	result := make(map[string][]websocketSession, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	pipe := cache.RDB.Pipeline()
	commands := make(map[string]*redis.MapStringStringCmd, len(userIDs))
	for _, userID := range userIDs {
		commands[userID] = pipe.HGetAll(ctx, sessionRegistryKey(userID))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	cleanup := cache.RDB.Pipeline()
	cleanupNeeded := false
	for userID, command := range commands {
		values, err := command.Result()
		if err != nil {
			return nil, err
		}
		sessions, expired := parseSessions(values)
		result[userID] = sessions
		if len(expired) > 0 {
			cleanup.HDel(ctx, sessionRegistryKey(userID), expired...)
			cleanupNeeded = true
		}
	}
	if cleanupNeeded {
		if _, err := cleanup.Exec(ctx); err != nil {
			log.Printf("cleanup expired WebSocket sessions after chat fanout: %v", err)
		}
	}

	return result, nil
}

func loadActiveSessions(ctx context.Context, userID string) ([]websocketSession, error) {
	key := sessionRegistryKey(userID)
	values, err := cache.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	sessions, expired := parseSessions(values)
	if len(expired) > 0 {
		if err := cache.RDB.HDel(ctx, key, expired...).Err(); err != nil {
			log.Printf("remove expired WebSocket sessions for user %s: %v", userID, err)
		}
	}
	return sessions, nil
}

func parseSessions(values map[string]string) ([]websocketSession, []string) {
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
	return sessions, expired
}

func replicaIsActive(ctx context.Context, replicaID string) (bool, error) {
	count, err := cache.RDB.Exists(ctx, replicaRegistryKey(replicaID)).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func removeTargetSessions(ctx context.Context, replicaID string, targets []rabbitmq.DeliveryTarget) {
	for _, target := range targets {
		if err := removeSessions(ctx, target.UserID, target.ConnectionIDs); err != nil {
			log.Printf("remove sessions for unavailable WebSocket replica %s user %s: %v", replicaID, target.UserID, err)
		}
	}
}

func removeSessions(ctx context.Context, userID string, connectionIDs []string) error {
	if len(connectionIDs) == 0 {
		return nil
	}
	return cache.RDB.HDel(ctx, sessionRegistryKey(userID), connectionIDs...).Err()
}

func chatMembersCacheKey(chatID string) string {
	return "chat:members:" + chatID
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
