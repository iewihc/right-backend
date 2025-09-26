package infra

import (
	"fmt"
	"log"

	"github.com/streadway/amqp"
)

type RabbitMQConfig struct {
	URL string
}

type RabbitMQ struct {
	Connection *amqp.Connection
	Channel    *amqp.Channel
}

func NewRabbitMQ(config RabbitMQConfig) (*RabbitMQ, error) {
	conn, err := amqp.Dial(config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	// 自動宣告所有隊列
	for _, queueName := range GetAllQueueNames() {
		_, err = ch.QueueDeclare(
			queueName.String(), // name
			true,               // durable
			false,              // delete when unused
			false,              // exclusive
			false,              // no-wait
			nil,                // arguments
		)
		if err != nil {
			ch.Close()
			conn.Close()
			return nil, fmt.Errorf("failed to declare queue %s: %w", queueName, err)
		}
	}

	log.Println("Connected to RabbitMQ!")

	return &RabbitMQ{
		Connection: conn,
		Channel:    ch,
	}, nil
}

func (r *RabbitMQ) Close() error {
	if r.Channel != nil {
		r.Channel.Close()
	}
	if r.Connection != nil {
		return r.Connection.Close()
	}
	return nil
}

func (r *RabbitMQ) DeclareQueue(name string) (amqp.Queue, error) {
	return r.Channel.QueueDeclare(
		name,  // name
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
}

func (r *RabbitMQ) PublishMessage(queueName string, body []byte) error {
	return r.Channel.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
}
