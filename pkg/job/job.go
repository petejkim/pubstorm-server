package job

import (
	"encoding/json"

	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/streadway/amqp"
)

type Job struct {
	Queue string
	Data  []byte
}

func New(queue string, data []byte) *Job {
	return &Job{Queue: queue, Data: data}
}

func NewWithJSON(queue string, data map[string]interface{}) (*Job, error) {
	d, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Job{Queue: queue, Data: d}, nil
}

func (j *Job) Enqueue() error {
	mq, err := mqconn.MQ()
	if err != nil {
		return err
	}

	ch, err := mq.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		j.Queue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return err
	}

	return ch.Publish(
		"",     // exchange
		q.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "text/plain",
			Body:         []byte(j.Data),
		},
	)
}
