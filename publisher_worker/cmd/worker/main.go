package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/rabbitmq"
	"github.com/subosito/gotenv"
)

const batchSize = 10

func connectRabbitMQ(ctx context.Context, url string) (*rabbitmq.Publisher, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	for {
		publisher, err := rabbitmq.NewPublisher(url)
		if err == nil {
			return publisher, nil
		}

		fmt.Fprintf(os.Stderr, "RabbitMQ unavailable: %v; retrying in 5s\n", err)
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func ProcessNotifications(ctx context.Context, publisher *rabbitmq.Publisher) error {
	tx, err := db.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	notificationOutbox := &models.UserNotificationOutbox{}
	notifications, err := notificationOutbox.GetManyWithLock(tx, ctx, batchSize)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if err := publisher.Publish(ctx, notification); err != nil {
			return err
		}
		if err := notification.Delete(tx, ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := gotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}
		if parentErr := gotenv.Load("../.env"); parentErr != nil && !os.IsNotExist(parentErr) {
			panic(parentErr)
		}
	}
	if err := db.InitDB(); err != nil {
		panic(err)
	}

	publisher, err := connectRabbitMQ(ctx, os.Getenv("RABBITMQ_URL"))
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		panic(err)
	}
	defer publisher.Close()

	for {
		if err := ProcessNotifications(ctx, publisher); err != nil {
			if ctx.Err() != nil {
				return
			}
			panic(fmt.Errorf("process notifications: %w", err))
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return
		}
	}
}
