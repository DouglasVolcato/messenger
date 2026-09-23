package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	realtimeExchangeName   = "notifications.realtime"
	deadLetterExchangeName = "notifications.deadletter"
	dlqQueueName           = "notifications.dlq"
)

type Event struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Type      string  `json:"type"`
	Title     *string `json:"title"`
	Content   string  `json:"content"`
	ActionURL *string `json:"action_url"`
}

type Handler func(context.Context, Event) error

type Consumer struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	queueName  string
}

func NewConsumer(url string) (*Consumer, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}

	if err := channel.ExchangeDeclare(realtimeExchangeName, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare realtime notification exchange: %w", err)
	}
	if err := channel.ExchangeDeclare(deadLetterExchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare notification dead-letter exchange: %w", err)
	}
	if _, err := channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare dlq queue: %w", err)
	}
	if err := channel.QueueBind(dlqQueueName, dlqQueueName, deadLetterExchangeName, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("bind dlq queue: %w", err)
	}

	queue, err := channel.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    deadLetterExchangeName,
			"x-dead-letter-routing-key": dlqQueueName,
		},
	)
	if err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare replica notification queue: %w", err)
	}
	if err := channel.QueueBind(queue.Name, "", realtimeExchangeName, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("bind replica notification queue %q: %w", queue.Name, err)
	}
	if err := channel.Qos(64, 0, false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("configure notification prefetch: %w", err)
	}

	return &Consumer{connection: connection, channel: channel, queueName: queue.Name}, nil
}

func (c *Consumer) QueueName() string {
	return c.queueName
}

func (c *Consumer) Close() error {
	channelErr := c.channel.Close()
	connectionErr := c.connection.Close()
	if channelErr != nil {
		return channelErr
	}
	return connectionErr
}

func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	deliveries, err := c.channel.Consume(c.queueName, "", false, true, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume replica notification queue %q: %w", c.queueName, err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("RabbitMQ delivery channel closed")
			}

			var event Event
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				log.Printf("WebSocket replica rejected malformed realtime event message_id=%q: %v", delivery.MessageId, err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter malformed realtime event message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if event.UserID == "" {
				log.Printf("WebSocket replica rejected realtime event message_id=%q without user_id", delivery.MessageId)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter realtime event without user_id message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if err := handler(ctx, event); err != nil {
				log.Printf("WebSocket replica failed realtime event message_id=%q user_id=%q: %v", delivery.MessageId, event.UserID, err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter failed realtime event message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("ack realtime event message_id=%q: %w", delivery.MessageId, err)
			}
		}
	}
}
