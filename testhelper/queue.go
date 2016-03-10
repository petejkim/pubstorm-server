package testhelper

import (
	"time"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func DeleteQueue(mq *amqp.Connection, queues ...string) {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()
	for _, queue := range queues {
		_, err = ch.QueueDelete(queue, false, false, false)
		Expect(err).To(BeNil())
	}
}

func ConsumeQueue(mq *amqp.Connection, queue string) *amqp.Delivery {
	ch, err := mq.Channel()
	Expect(err).To(BeNil())
	defer ch.Close()

	msgCh, err := ch.Consume(
		queue, // queue
		"",    // consumer
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)

	select {
	case m := <-msgCh:
		return &m
	case <-time.After(100 * time.Millisecond):
		ginkgo.Fail("Could not consume message before timeout")
	}

	return nil
}
