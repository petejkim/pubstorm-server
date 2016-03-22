package pubsub_test

import (
	"testing"

	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/testhelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pubsub")
}

var _ = Describe("PubSub", func() {
	Describe("NewMessageWithJSON()", func() {
		It("encodes json and returns a new job", func() {
			m, err := pubsub.NewMessageWithJSON("foo-exchange", "bar-route", map[string]interface{}{
				"foo": "bar",
				"baz": "qux",
			})
			Expect(err).To(BeNil())

			Expect(m).To(BeAssignableToTypeOf(&pubsub.Message{}))
			Expect(m.ExchangeName).To(Equal("foo-exchange"))
			Expect(m.Route).To(Equal("bar-route"))
			Expect(m.Data).To(MatchJSON(`
				{
					"foo": "bar",
					"baz": "qux"
				}
			`))
		})
	})

	Describe("Message.Publish()", func() {
		var (
			mq       *amqp.Connection
			q1, q2   string
			pm       *pubsub.Message
			exchange string
			err      error
		)

		BeforeEach(func() {
			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			exchange = "foo-exchange"
			route := "bar-route"

			pm = pubsub.NewMessage(exchange, route, []byte("chocolates"))

			q1 = testhelper.StartQueueWithExchange(mq, exchange, route)
			q2 = testhelper.StartQueueWithExchange(mq, exchange, route)
		})

		AfterEach(func() {
			testhelper.DeleteQueue(mq, q1, q2)
			testhelper.DeleteExchange(mq, exchange)
		})

		It("enqueues job to queue", func() {
			err := pm.Publish()
			Expect(err).To(BeNil())

			d1 := testhelper.ConsumeQueue(mq, q1)
			Expect(string(d1.Body)).To(Equal("chocolates"))

			d2 := testhelper.ConsumeQueue(mq, q2)
			Expect(string(d2.Body)).To(Equal("chocolates"))
		})
	})
})
