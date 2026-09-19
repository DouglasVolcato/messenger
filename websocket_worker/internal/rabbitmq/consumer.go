package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"
	exchangeName   = "notifications"
	dlqQueueName   = "notifications.dlq"
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

		if _, err = channel.QueueDeclare(dlqQueueName, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlq queue: %w", err)
		}
		if err := channel.QueueBind(dlqQueueName, dlqQueueName, exchangeName, false, nil); err != nil {
			return fmt.Errorf("bind dlq queue: %w", err)
		}
		queueArgs := amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": dlqQueueName,
		}

		if _, err := channel.QueueDeclare(priorityQueue.Name, true, false, false, false, queueArgs); err != nil {
			return fmt.Errorf("declare %s priority queue %q: %w", priorityQueue.Priority, priorityQueue.Name, err)
		}

		deliveries, err := channel.Consume(priorityQueue.Name, "", false, false, false, false, queueArgs)
		if err != nil {
			return fmt.Errorf("consume %s priority queue %q: %w", priorityQueue.Priority, priorityQueue.Name, err)
		}

		workers.Add(1)
		go func(priorityQueue PriorityQueue, deliveries <-chan amqp.Delivery) {
			defer workers.Done()
			consumeLoop(ctx, priorityQueue, deliveries, handler)
		}(priorityQueue, deliveries)
	}

	workers.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("RabbitMQ consumer stopped unexpectedly")
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
				log.Printf("WebSocket worker failed to process %s priority delivery message_id=%q: %v", priorityQueue.Priority, delivery.MessageId, err)
				_ = delivery.Nack(false, true)
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return
			}
		}
	}
}
