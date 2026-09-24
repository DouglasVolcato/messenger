package cache

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func InitRedisClient() error {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return fmt.Errorf("REDIS_URL is required")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	RDB = redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := RDB.Ping(ctx).Err(); err != nil {
		_ = RDB.Close()
		RDB = nil
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	return nil
}

func CloseRedisClient() error {
	if RDB != nil {
		return RDB.Close()
	}
	return nil
}
