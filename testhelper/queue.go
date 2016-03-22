package testhelper

import (
	"time"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func DeleteQueue(mq *amqp.Connection, queueNames ...string) {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()
	for _, queueName := range queueNames {
		_, err = ch.QueueDelete(queueName, false, false, false)
		Expect(err).To(BeNil())
	}
}

func ConsumeQueue(mq *amqp.Connection, queueName string) *amqp.Delivery {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()

	msgCh, err := ch.Consume(
		queueName, // queue
		"",        // consumer
		true,      // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)

	select {
	case m := <-msgCh:
		return &m
	case <-time.After(100 * time.Millisecond):
		ginkgo.Fail("Could not consume message before timeout")
	}

	return nil
}

func DeleteExchange(mq *amqp.Connection, exchangeNames ...string) {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()

	for _, exchangeName := range exchangeNames {
		err = ch.ExchangeDelete(exchangeName, false, false)
		Expect(err).To(BeNil())
	}
}

func StartQueueWithExchange(mq *amqp.Connection, exchangeName, route string) string {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()

	// This is to make sure the exchange exists
	err = ch.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	Expect(err).To(BeNil())

	// Create anonymous queue
	q, err := ch.QueueDeclare(
		"",    // name
		false, // durable
		true,  // delete when usused
		false, // exclusive. This should be false to make connection persistent
		false, // no-wait
		nil,   // arguments
	)

	Expect(err).To(BeNil())

	ch.QueueBind(
		q.Name,       // queue name
		route,        // routing key
		exchangeName, // exchange
		false,
		nil)

	Expect(err).To(BeNil())

	return q.Name
}
