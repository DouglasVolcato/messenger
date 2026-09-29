package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	deliveryExchangeName   = "websocket.delivery"
	deadLetterExchangeName = "notifications.deadletter"
	dlqQueueName           = "notifications.dlq"
)

type Notification struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Title     *string `json:"title"`
	Content   string  `json:"content"`
	ActionURL *string `json:"action_url"`
}

type DeliveryTarget struct {
	UserID        string   `json:"user_id"`
	ConnectionIDs []string `json:"connection_ids"`
}

type Delivery struct {
	UserID        string           `json:"user_id,omitempty"`
	ConnectionIDs []string         `json:"connection_ids,omitempty"`
	Targets       []DeliveryTarget `json:"targets,omitempty"`
	Notification  Notification     `json:"notification"`
}

func (d Delivery) EffectiveTargets() []DeliveryTarget {
	if len(d.Targets) > 0 {
		return d.Targets
	}
	if d.UserID == "" || len(d.ConnectionIDs) == 0 {
		return nil
	}
	return []DeliveryTarget{{UserID: d.UserID, ConnectionIDs: d.ConnectionIDs}}
}

type Handler func(context.Context, Delivery) error

type Consumer struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	queueName  string
}

func NewConsumer(url, replicaID string) (*Consumer, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}
	if strings.TrimSpace(replicaID) == "" {
		return nil, fmt.Errorf("replica ID is required")
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

	cleanup := func() {
		_ = channel.Close()
		_ = connection.Close()
	}

	if err := channel.ExchangeDeclare(deliveryExchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("declare WebSocket delivery exchange: %w", err)
	}
	if err := channel.ExchangeDeclare(deadLetterExchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("declare notification dead-letter exchange: %w", err)
	}
	if _, err := channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("declare notification DLQ: %w", err)
	}
	if err := channel.QueueBind(dlqQueueName, dlqQueueName, deadLetterExchangeName, false, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("bind notification DLQ: %w", err)
	}

	queueName := "websocket.delivery." + sanitizeQueuePart(replicaID)
	queue, err := channel.QueueDeclare(
		queueName,
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
		cleanup()
		return nil, fmt.Errorf("declare WebSocket replica queue %q: %w", queueName, err)
	}
	if err := channel.QueueBind(queue.Name, replicaID, deliveryExchangeName, false, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("bind WebSocket replica queue %q: %w", queue.Name, err)
	}
	if err := channel.Qos(64, 0, false); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure WebSocket replica queue prefetch: %w", err)
	}

	return &Consumer{
		connection: connection,
		channel:    channel,
		queueName:  queue.Name,
	}, nil
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
		return fmt.Errorf("consume WebSocket replica queue %q: %w", c.queueName, err)
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
				return errors.New("RabbitMQ WebSocket delivery channel closed")
			}

			var command Delivery
			if err := json.Unmarshal(delivery.Body, &command); err != nil {
				log.Printf("WebSocket replica rejected malformed delivery message_id=%q: %v", delivery.MessageId, err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter malformed WebSocket delivery message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if len(command.EffectiveTargets()) == 0 || command.Notification.ID == "" {
				log.Printf("WebSocket replica rejected incomplete delivery message_id=%q", delivery.MessageId)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter incomplete WebSocket delivery message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if err := handler(ctx, command); err != nil {
				log.Printf("WebSocket replica failed delivery message_id=%q user_id=%q: %v", delivery.MessageId, command.UserID, err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("dead-letter failed WebSocket delivery message_id=%q: %w", delivery.MessageId, nackErr)
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("ack WebSocket delivery message_id=%q: %w", delivery.MessageId, err)
			}
		}
	}
}

func sanitizeQueuePart(value string) string {
	var builder strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '-', char == '_', char == '.':
			builder.WriteRune(char)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
