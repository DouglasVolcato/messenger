package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	realtimeExchangeName   = "notifications.realtime"
	deadLetterExchangeName = "notifications.deadletter"
	dlqQueueName           = "notifications.dlq"
)

var supportedTypes = map[string]struct{}{
	"DIRECT_MESSAGE":     {},
	"CHAT_MESSAGE":       {},
	"COMPANY_MEMBERSHIP": {},
}

type Publisher struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	confirms   <-chan amqp.Confirmation
}

func NewPublisher(url string) (*Publisher, error) {
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
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	confirms := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

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
	if _, err = channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare dlq queue: %w", err)
	}
	if err := channel.QueueBind(dlqQueueName, dlqQueueName, deadLetterExchangeName, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("bind dlq queue: %w", err)
	}

	return &Publisher{connection: connection, channel: channel, confirms: confirms}, nil
}

func (p *Publisher) Close() error {
	if err := p.channel.Close(); err != nil {
		_ = p.connection.Close()
		return err
	}
	return p.connection.Close()
}

func (p *Publisher) Publish(ctx context.Context, notification models.UserNotificationOutbox) error {
	body, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification %q: %w", notification.ID, err)
	}

	if _, ok := supportedTypes[notification.Type]; !ok {
		headers := amqp.Table{
			"x-dlq-source": "publisher_worker",
			"x-dlq-reason": "unsupported_notification_type",
		}
		if err := p.publishConfirmed(ctx, deadLetterExchangeName, dlqQueueName, notification, body, headers, true); err != nil {
			return fmt.Errorf("publish unsupported notification %q to DLQ: %w", notification.ID, err)
		}
		log.Printf("Publisher worker moved notification message_id=%q type=%q to DLQ: unsupported notification type", notification.ID, notification.Type)
		return nil
	}

	if err := p.publishConfirmed(ctx, realtimeExchangeName, "", notification, body, nil, false); err != nil {
		return fmt.Errorf("publish realtime notification %q: %w", notification.ID, err)
	}
	return nil
}

func (p *Publisher) publishConfirmed(
	ctx context.Context,
	exchange string,
	routingKey string,
	notification models.UserNotificationOutbox,
	body []byte,
	headers amqp.Table,
	mandatory bool,
) error {
	if err := p.channel.PublishWithContext(ctx, exchange, routingKey, mandatory, false, amqp.Publishing{
		Headers:      headers,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    notification.ID,
		Type:         notification.Type,
		Body:         body,
	}); err != nil {
		return err
	}

	select {
	case confirmation, ok := <-p.confirms:
		if !ok {
			return fmt.Errorf("RabbitMQ publisher confirms channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("RabbitMQ rejected message %q", notification.ID)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
