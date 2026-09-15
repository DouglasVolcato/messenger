package cache

import (
	"fmt"
	"os"

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
		return fmt.Errorf("Failed to parse Redis URL: %v", err)
	}
	RDB = redis.NewClient(opt)
	return nil
}

func CloseRedisClient() error {
	if RDB != nil {
		return RDB.Close()
	}
	return nil
}
