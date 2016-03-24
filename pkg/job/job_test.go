package job_test

import (
	"testing"

	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/testhelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "job")
}

var _ = Describe("Job", func() {
	Describe("NewWithJSON()", func() {
		It("encodes json and returns a new job", func() {
			j, err := job.NewWithJSON("fooq", map[string]interface{}{
				"foo": "bar",
				"baz": "qux",
			})
			Expect(err).To(BeNil())

			Expect(j).To(BeAssignableToTypeOf(&job.Job{}))
			Expect(j.QueueName).To(Equal("fooq"))
			Expect(j.Data).To(MatchJSON(`
				{
					"foo": "bar",
					"baz": "qux"
				}
			`))
		})
	})

	Describe("Enqueue()", func() {
		var (
			mq  *amqp.Connection
			j   *job.Job
			err error
		)

		BeforeEach(func() {
			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			testhelper.DeleteQueue(mq, "fooq")
			j = job.New("fooq", []byte("bar"))
		})

		It("enqueues job to queue", func() {
			err := j.Enqueue()
			Expect(err).To(BeNil())

			d := testhelper.ConsumeQueue(mq, "fooq")
			Expect(d).NotTo(BeNil())
			Expect(string(d.Body)).To(Equal("bar"))
		})
	})
})
