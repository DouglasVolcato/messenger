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
	exchangeName       = "notifications"
	dlqQueueName       = "notifications.dlq"
	chatFanoutQueueName = "notifications.chat_fanout"
)

var queueByType = map[string]string{
	"DIRECT_MESSAGE":     "notifications.direct_message",
	"CHAT_MESSAGE":       "notifications.chat_message",
	"COMPANY_MEMBERSHIP": "notifications.company_membership",
}

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

	if _, err = channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("declare dlq queue: %w", err)
	}
	if err := channel.QueueBind(dlqQueueName, dlqQueueName, exchangeName, false, nil); err != nil {
		channel.Close()
		connection.Close()
		return nil, fmt.Errorf("bind dlq queue: %w", err)
	}

	queueArgs := amqp.Table{
		"x-dead-letter-exchange":    exchangeName,
		"x-dead-letter-routing-key": dlqQueueName,
	}
	queueNames := make([]string, 0, len(queueByType)+1)
	for _, queueName := range queueByType {
		queueNames = append(queueNames, queueName)
	}
	queueNames = append(queueNames, chatFanoutQueueName)
	for _, queueName := range queueNames {
		if _, err := channel.QueueDeclare(queueName, true, false, false, false, queueArgs); err != nil {
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
	body, err := json.Marshal(notificationEvent{
		ID:        notification.ID,
		UserID:    notification.UserID,
		Type:      notification.Type,
		Title:     notification.Title,
		Content:   notification.Content,
		ActionURL: notification.ActionURL,
	})
	if err != nil {
		return fmt.Errorf("marshal notification %q: %w", notification.ID, err)
	}

	queueName, ok := queueByType[notification.Type]
	if !ok {
		headers := amqp.Table{
			"x-dlq-source": "publisher_worker",
			"x-dlq-reason": "unsupported_notification_type",
		}
		if err := p.publishConfirmed(ctx, dlqQueueName, notification, body, headers); err != nil {
			return fmt.Errorf("publish unsupported notification %q to DLQ: %w", notification.ID, err)
		}
		log.Printf("Publisher worker moved notification message_id=%q type=%q to DLQ: unsupported notification type", notification.ID, notification.Type)
		return nil
	}

	if err := p.publishConfirmed(ctx, queueName, notification, body, nil); err != nil {
		return fmt.Errorf("publish notification %q: %w", notification.ID, err)
	}
	return nil
}

func (p *Publisher) PublishChatMessage(ctx context.Context, event models.ChatMessageOutbox) error {
	body, err := json.Marshal(notificationEvent{
		ID:           event.ID,
		ChatID:       event.ChatID,
		CompanyID:    event.CompanyID,
		SenderUserID: event.SenderUserID,
		MessageID:    event.MessageID,
		Type:         event.Type,
		Title:        event.Title,
		Content:      event.Content,
		ActionURL:    event.ActionURL,
	})
	if err != nil {
		return fmt.Errorf("marshal chat message event %q: %w", event.ID, err)
	}

	return p.publishRawConfirmed(ctx, chatFanoutQueueName, event.ID, event.Type, body, nil)
}

func (p *Publisher) publishConfirmed(
	ctx context.Context,
	routingKey string,
	notification models.UserNotificationOutbox,
	body []byte,
	headers amqp.Table,
) error {
	return p.publishRawConfirmed(ctx, routingKey, notification.ID, notification.Type, body, headers)
}

func (p *Publisher) publishRawConfirmed(
	ctx context.Context,
	routingKey, messageID, messageType string,
	body []byte,
	headers amqp.Table,
) error {
	if err := p.channel.PublishWithContext(ctx, exchangeName, routingKey, true, false, amqp.Publishing{
		Headers:      headers,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    messageID,
		Type:         messageType,
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
			return fmt.Errorf("RabbitMQ rejected message %q", messageID)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
