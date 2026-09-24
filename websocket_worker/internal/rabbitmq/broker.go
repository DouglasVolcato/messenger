package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

var ErrReplicaUnavailable = errors.New("WebSocket replica route unavailable")

const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"

	notificationExchangeName = "notifications"
	dlqQueueName             = "notifications.dlq"
	deliveryExchangeName     = "websocket.delivery"
)

type PriorityQueue struct {
	Priority string
	Name     string
}

var PriorityQueues = []PriorityQueue{
	{Priority: PriorityHigh, Name: "notifications.direct_message"},
	{Priority: PriorityNormal, Name: "notifications.chat_message"},
	{Priority: PriorityLow, Name: "notifications.company_membership"},
}

type Notification struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Title     *string `json:"title"`
	Content   string  `json:"content"`
	ActionURL *string `json:"action_url"`
}

type Delivery struct {
	UserID        string       `json:"user_id"`
	ConnectionIDs []string     `json:"connection_ids"`
	Notification  Notification `json:"notification"`
}

type Consumer struct {
	connection *amqp.Connection
}

type Handler func(context.Context, PriorityQueue, amqp.Delivery) error

func NewConsumer(url string) (*Consumer, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	return &Consumer{connection: connection}, nil
}

func (c *Consumer) Close() error {
	return c.connection.Close()
}

func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	channels := make([]*amqp.Channel, 0, len(PriorityQueues))
	defer func() {
		for _, channel := range channels {
			_ = channel.Close()
		}
	}()

	var workers sync.WaitGroup
	for _, priorityQueue := range PriorityQueues {
		channel, err := c.connection.Channel()
		if err != nil {
			return fmt.Errorf("open %s priority channel: %w", priorityQueue.Priority, err)
		}
		channels = append(channels, channel)

		if err := channel.Qos(1, 0, false); err != nil {
			return fmt.Errorf("configure %s priority prefetch: %w", priorityQueue.Priority, err)
		}
		if err := channel.ExchangeDeclare(notificationExchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare notification exchange: %w", err)
		}
		if _, err := channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare notification DLQ: %w", err)
		}
		if err := channel.QueueBind(dlqQueueName, dlqQueueName, notificationExchangeName, false, nil); err != nil {
			return fmt.Errorf("bind notification DLQ: %w", err)
		}

		queueArgs := amqp.Table{
			"x-dead-letter-exchange":    notificationExchangeName,
			"x-dead-letter-routing-key": dlqQueueName,
		}
		if _, err := channel.QueueDeclare(priorityQueue.Name, true, false, false, false, queueArgs); err != nil {
			return fmt.Errorf("declare %s priority queue %q: %w", priorityQueue.Priority, priorityQueue.Name, err)
		}
		if err := channel.QueueBind(priorityQueue.Name, priorityQueue.Name, notificationExchangeName, false, nil); err != nil {
			return fmt.Errorf("bind %s priority queue %q: %w", priorityQueue.Priority, priorityQueue.Name, err)
		}

		deliveries, err := channel.Consume(priorityQueue.Name, "", false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("consume %s priority queue %q: %w", priorityQueue.Priority, priorityQueue.Name, err)
		}

		workers.Add(1)
		go func(queue PriorityQueue, messages <-chan amqp.Delivery) {
			defer workers.Done()
			consumeLoop(ctx, queue, messages, handler)
		}(priorityQueue, deliveries)
	}

	workers.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("RabbitMQ notification consumer stopped unexpectedly")
}

func consumeLoop(ctx context.Context, priorityQueue PriorityQueue, deliveries <-chan amqp.Delivery, handler Handler) {
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveries:
			if !ok {
				return
			}
			if err := handler(ctx, priorityQueue, delivery); err != nil {
				log.Printf("WebSocket worker failed %s delivery message_id=%q: %v", priorityQueue.Priority, delivery.MessageId, err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					log.Printf("WebSocket worker failed to dead-letter %s delivery message_id=%q: %v", priorityQueue.Priority, delivery.MessageId, nackErr)
					return
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				log.Printf("WebSocket worker failed to ack %s delivery message_id=%q: %v", priorityQueue.Priority, delivery.MessageId, err)
				return
			}
		}
	}
}

type Router struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	confirms   <-chan amqp.Confirmation
	returns    <-chan amqp.Return
	mu         sync.Mutex
}

func NewRouter(url string) (*Router, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect RabbitMQ delivery router: %w", err)
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open RabbitMQ delivery router channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("enable RabbitMQ delivery confirms: %w", err)
	}
	if err := channel.ExchangeDeclare(deliveryExchangeName, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("declare WebSocket delivery exchange: %w", err)
	}

	return &Router{
		connection: connection,
		channel:    channel,
		confirms:   channel.NotifyPublish(make(chan amqp.Confirmation, 1)),
		returns:    channel.NotifyReturn(make(chan amqp.Return, 1)),
	}, nil
}

func (r *Router) Close() error {
	channelErr := r.channel.Close()
	connectionErr := r.connection.Close()
	if channelErr != nil {
		return channelErr
	}
	return connectionErr
}

func (r *Router) Publish(ctx context.Context, replicaID string, delivery Delivery) error {
	if replicaID == "" {
		return fmt.Errorf("replica ID is required")
	}
	if delivery.UserID == "" || len(delivery.ConnectionIDs) == 0 || delivery.Notification.ID == "" {
		return fmt.Errorf("incomplete WebSocket delivery")
	}

	body, err := json.Marshal(delivery)
	if err != nil {
		return fmt.Errorf("marshal WebSocket delivery: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.channel.PublishWithContext(ctx, deliveryExchangeName, replicaID, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Transient,
		MessageId:    delivery.Notification.ID,
		Type:         delivery.Notification.Type,
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish WebSocket delivery to replica %q: %w", replicaID, err)
	}

	var returned *amqp.Return
	for {
		select {
		case message, ok := <-r.returns:
			if !ok {
				return errors.New("RabbitMQ delivery returns channel closed")
			}
			returned = &message
		case confirmation, ok := <-r.confirms:
			if !ok {
				return errors.New("RabbitMQ delivery confirms channel closed")
			}
			if !confirmation.Ack {
				return fmt.Errorf("RabbitMQ rejected WebSocket delivery %q", delivery.Notification.ID)
			}
			if returned != nil {
				return fmt.Errorf("%w: replica=%q message=%q reply=%d %s",
					ErrReplicaUnavailable, replicaID, returned.MessageId, returned.ReplyCode, returned.ReplyText)
			}
			// RabbitMQ sends basic.return before the publisher confirm for an
			// unroutable mandatory message. Drain a return already queued by
			// the client before considering the delivery routed.
			select {
			case message, ok := <-r.returns:
				if ok {
					return fmt.Errorf("%w: replica=%q message=%q reply=%d %s",
						ErrReplicaUnavailable, replicaID, message.MessageId, message.ReplyCode, message.ReplyText)
				}
			default:
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
