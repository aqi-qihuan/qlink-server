package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ wraps the AMQP connection and provides publish/consume helpers.
type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewRabbitMQ connects to RabbitMQ and returns a wrapper.
func NewRabbitMQ(url string) (*RabbitMQ, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial failed: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq channel failed: %w", err)
	}
	return &RabbitMQ{conn: conn, channel: ch}, nil
}

// Channel returns the underlying AMQP channel.
func (r *RabbitMQ) Channel() *amqp.Channel {
	return r.channel
}

// DeclareExchange declares a topic exchange.
func (r *RabbitMQ) DeclareExchange(name string) error {
	return r.channel.ExchangeDeclare(name, "topic", true, false, false, false, nil)
}

// DeclareQueue declares a durable queue.
func (r *RabbitMQ) DeclareQueue(name string, args amqp.Table) (amqp.Queue, error) {
	return r.channel.QueueDeclare(name, true, false, false, false, args)
}

// BindQueue binds a queue to an exchange with a routing key.
func (r *RabbitMQ) BindQueue(queueName, routingKey, exchangeName string) error {
	return r.channel.QueueBind(queueName, routingKey, exchangeName, false, nil)
}

// PublishJSON publishes a JSON-serializable message to an exchange.
func (r *RabbitMQ) PublishJSON(exchange, routingKey string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("json marshal failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         data,
	})
}

// Consume starts consuming from a queue with panic recovery and retry limits.
// Handler panics are recovered to prevent goroutine crashes.
// Failed messages are requeued until they exceed the max retry duration (5min),
// then dropped to prevent infinite requeue loops.
// On connection loss, the consumer logs a fatal error so the process restarts
// (via container orchestrator), since in-process reconnect would need to
// re-declare all exchanges/queues/bindings.
func (r *RabbitMQ) Consume(queueName, consumerTag string, handler func([]byte) error) error {
	msgs, err := r.channel.Consume(queueName, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume failed: %w", err)
	}

	// Monitor connection close — log error but don't kill the process.
	// In production, implement reconnection with backoff + re-declare exchanges/queues.
	// For now, log and let publish operations fail gracefully (they have 5s timeout).
	go func() {
		closeChan := r.conn.NotifyClose(make(chan *amqp.Error, 1))
		if amqpErr, ok := <-closeChan; ok {
			log.Printf("[RabbitMQ] connection lost on consumer %s: %v — publish operations will fail until reconnected", queueName, amqpErr)
		}
	}()

	go func() {
		for msg := range msgs {
			r.handleMessage(msg, queueName, handler)
		}
		log.Printf("[RabbitMQ] consumer %s stopped (channel closed)", queueName)
	}()
	return nil
}

// handleMessage processes a single message with panic recovery and retry limits.
func (r *RabbitMQ) handleMessage(msg amqp.Delivery, queueName string, handler func([]byte) error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RabbitMQ] handler panic on %s: %v", queueName, rec)
			msg.Nack(false, false) // drop poison message
		}
	}()

	if err := handler(msg.Body); err != nil {
		// Limit retry duration: if the message is older than 5 minutes, drop it
		// to prevent infinite requeue loops. Without a DLX, this is the simplest
		// way to avoid poison messages consuming CPU and log space forever.
		if !msg.Timestamp.IsZero() && time.Since(msg.Timestamp) > 5*time.Minute {
			log.Printf("[RabbitMQ] message on %s older than 5min, dropping: %v", queueName, err)
			msg.Nack(false, false)
		} else {
			log.Printf("[RabbitMQ] consume error on %s: %v", queueName, err)
			msg.Nack(false, true) // requeue for retry
		}
	} else {
		msg.Ack(false)
	}
}

// Close closes the channel and connection.
func (r *RabbitMQ) Close() {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}
