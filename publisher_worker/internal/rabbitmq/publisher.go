package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

const exchangeName = "notifications"

var queueByType = map[string]string{
	"DIRECT_MESSAGE":     "notifications.direct_message",
	"CHAT_MESSAGE":       "notifications.chat_message",
	"COMPANY_MEMBERSHIP": "notifications.company_membership",
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
		connection.Close()
		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	confirms := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	if err := channel.ExchangeDeclare(exchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("declare notification exchange: %w", err)
	}
	for _, queueName := range queueByType {
		if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
			channel.Close()
			connection.Close()
			return nil, fmt.Errorf("declare notification queue %q: %w", queueName, err)
		}
		if err := channel.QueueBind(queueName, queueName, exchangeName, false, nil); err != nil {
			channel.Close()
			connection.Close()
			return nil, fmt.Errorf("bind notification queue %q: %w", queueName, err)
		}
	}

	return &Publisher{connection: connection, channel: channel, confirms: confirms}, nil
}

func (p *Publisher) Close() error {
	if err := p.channel.Close(); err != nil {
		p.connection.Close()
		return err
	}
	return p.connection.Close()
}

func (p *Publisher) Publish(ctx context.Context, notification models.UserNotificationOutbox) error {
	queueName, ok := queueByType[notification.Type]
	if !ok {
		return fmt.Errorf("unsupported notification type %q", notification.Type)
	}

	body, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification %q: %w", notification.ID, err)
	}
	if err := p.channel.PublishWithContext(ctx, exchangeName, queueName, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    notification.ID,
		Type:         notification.Type,
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish notification %q: %w", notification.ID, err)
	}

	select {
	case confirmation := <-p.confirms:
		if !confirmation.Ack {
			return fmt.Errorf("RabbitMQ rejected notification %q", notification.ID)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
