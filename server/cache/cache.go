package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
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

func SetUserCache(ctx context.Context, user models.User) error {
	key := fmt.Sprintf("user:%s", user.ID)
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetUserCache(ctx context.Context, userID string) (*models.User, error) {
	key := fmt.Sprintf("user:%s", userID)
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var user models.User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		return nil, err
	}
	return &user, nil
}
func DeleteUserCache(ctx context.Context, userID string) error {
	key := fmt.Sprintf("user:%s", userID)
	return RDB.Del(ctx, key).Err()
}

func SetManyUsersCache(ctx context.Context, users []models.User, key string) error {
	data, err := json.Marshal(users)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetManyUsersCache(ctx context.Context, key string) ([]models.User, error) {
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var users []models.User
	if err := json.Unmarshal([]byte(data), &users); err != nil {
		return nil, err
	}
	return users, nil
}
func DeleteManyUsersCache(ctx context.Context, key string) error {
	return RDB.Del(ctx, key).Err()
}

func SetChatsCache(ctx context.Context, chats []models.Chat, key string) error {
	data, err := json.Marshal(chats)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetChatsCache(ctx context.Context, key string) ([]models.Chat, error) {
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var chats []models.Chat
	if err := json.Unmarshal([]byte(data), &chats); err != nil {
		return nil, err
	}
	return chats, nil
}
func DeleteChatsCache(ctx context.Context, key string) error {
	return RDB.Del(ctx, key).Err()
}

func SetManyMessagesCache(ctx context.Context, messages []models.Message, key string) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetManyMessagesCache(ctx context.Context, key string) ([]models.Message, error) {
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var messages []models.Message
	if err := json.Unmarshal([]byte(data), &messages); err != nil {
		return nil, err
	}
	return messages, nil
}
func DeleteManyMessagesCache(ctx context.Context, key string) error {
	return RDB.Del(ctx, key).Err()
}

func SetCompanyCache(ctx context.Context, company models.Company) error {
	key := fmt.Sprintf("company:%s", company.ID)
	data, err := json.Marshal(company)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetCompanyCache(ctx context.Context, companyID string) (*models.Company, error) {
	key := fmt.Sprintf("company:%s", companyID)
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var company models.Company
	if err := json.Unmarshal([]byte(data), &company); err != nil {
		return nil, err
	}
	return &company, nil
}
func DeleteCompanyCache(ctx context.Context, companyID string) error {
	key := fmt.Sprintf("company:%s", companyID)
	return RDB.Del(ctx, key).Err()
}

func SetCompanyUsersCache(ctx context.Context, users []models.User, companyID string) error {
	key := fmt.Sprintf("company:%s:users", companyID)
	data, err := json.Marshal(users)
	if err != nil {
		return err
	}
	return RDB.Set(
		ctx,
		key,
		data,
		5*time.Minute,
	).Err()
}
func GetCompanyUsersCache(ctx context.Context, companyID string) ([]models.User, error) {
	key := fmt.Sprintf("company:%s:users", companyID)
	data, err := RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var users []models.User
	if err := json.Unmarshal([]byte(data), &users); err != nil {
		return nil, err
	}
	return users, nil
}
func DeleteCompanyUsersCache(ctx context.Context, companyID string) error {
	key := fmt.Sprintf("company:%s:users", companyID)
	return RDB.Del(ctx, key).Err()
}
