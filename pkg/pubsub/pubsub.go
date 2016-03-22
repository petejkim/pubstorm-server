package pubsub

import (
	"encoding/json"
	"time"

	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/streadway/amqp"
)

type Message struct {
	ExchangeName string
	Route        string
	Data         []byte
}

func NewMessage(exchangeName, route string, data []byte) *Message {
	return &Message{ExchangeName: exchangeName, Route: route, Data: data}
}

func NewMessageWithJSON(exchangeName, route string, data interface{}) (*Message, error) {
	d, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Message{ExchangeName: exchangeName, Route: route, Data: d}, nil
}

func (j *Message) Publish() error {
	mq, err := mqconn.MQ()
	if err != nil {
		return err
	}

	ch, err := mq.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	// This is to make sure the exchange exists
	err = ch.ExchangeDeclare(
		j.ExchangeName, // name
		"direct",       // type
		true,           // durable
		false,          // auto-deleted
		false,          // internal
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		return err
	}

	return ch.Publish(
		j.ExchangeName, // exchange
		j.Route,        // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "text/plain",
			Body:         []byte(j.Data),
			Timestamp:    time.Now(),
		},
	)
}
