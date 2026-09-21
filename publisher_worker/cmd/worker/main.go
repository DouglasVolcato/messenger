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
	"github.com/douglasvolcato/messager-architecture-challenge/internal/metrics"
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
			fmt.Fprintf(os.Stderr, "Connected to RabbitMQ at %s\n", url)
			return publisher, nil
		}

		metrics.IncRabbitReconnect()
		fmt.Fprintf(os.Stderr, "RabbitMQ unavailable: %v; retrying in 5s\n", err)
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "context canceled while waiting for RabbitMQ: %v\n", ctx.Err())
			return nil, ctx.Err()
		}
	}
}

func ProcessNotifications(ctx context.Context, publisher *rabbitmq.Publisher) error {
	started := time.Now()

	tx, err := db.BeginTransaction(ctx)
	if err != nil {
		metrics.IncProcessingError()
		return err
	}
	defer tx.Rollback()

	notificationOutbox := &models.UserNotificationOutbox{}
	notifications, err := notificationOutbox.GetManyWithLock(tx, ctx, batchSize)
	if err != nil {
		metrics.IncProcessingError()
		return err
	}
	for _, notification := range notifications {
		if err := publisher.Publish(ctx, notification); err != nil {
			metrics.IncProcessingError()
			return err
		}
		metrics.IncPublished(notification.Type)
		if err := notification.Delete(tx, ctx); err != nil {
			metrics.IncProcessingError()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		metrics.IncProcessingError()
		return err
	}
	metrics.ObserveBatch(len(notifications), time.Since(started))
	return nil
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

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9090"
	}
	go metrics.Serve(ctx, ":"+metricsPort, db.DB)

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
