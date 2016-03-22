package invalidator_test

import (
	"net/http"
	"testing"

	"github.com/nitrous-io/rise-server/edged/invalidator"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "invalidator")
}

var _ = Describe("Invalidator", func() {
	var (
		origAPIHost string
		server      *ghttp.Server
	)

	BeforeEach(func() {
		server = ghttp.NewServer()
		origAPIHost = invalidator.APIHost
		invalidator.APIHost = server.URL()
	})

	AfterEach(func() {
		invalidator.APIHost = origAPIHost
		server.Close()
	})

	Describe("Work", func() {
		It("makes invalidation request", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/invalidate/foo-bar-express.rise.cloud"),
					ghttp.RespondWith(http.StatusOK, `{ "invalidated": true }`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/invalidate/www.foo-bar-express.com"),
					ghttp.RespondWith(http.StatusOK, `{ "invalidated": true }`),
				),
			)

			err := invalidator.Work([]byte(`{
				"domains": [
					"foo-bar-express.rise.cloud",
					"www.foo-bar-express.com"
				]
			}`))

			Expect(server.ReceivedRequests()).To(HaveLen(2))
			Expect(err).To(BeNil())
		})
	})
})
