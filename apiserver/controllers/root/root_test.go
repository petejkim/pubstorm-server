package root_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nitrous-io/rise-server/apiserver/server"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "root")
}

var _ = Describe("Root", func() {
	var (
		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		s = httptest.NewServer(server.New())
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("global middleware", func() {
		Describe("CORS", func() {
			Context("when a preflight request is made", func() {
				It("responds with CORS headers", func() {
					req, err := http.NewRequest("OPTIONS", s.URL+"/", nil)
					Expect(err).To(BeNil())

					req.Header.Set("Origin", "https://www.example.com/")
					req.Header.Set("Access-Control-Request-Method", "POST")
					req.Header.Set("Access-Control-Request-Headers", "Accept,Authorization")
					req.Header.Add("Access-Control-Request-Headers", "Content-Type")

					res, err = http.DefaultClient.Do(req)
					Expect(err).To(BeNil())

					Expect(res.Header.Get("Access-Control-Allow-Origin")).To(Equal("*"))
					Expect(res.Header.Get("Access-Control-Allow-Methods")).To(Equal("GET,HEAD,POST,PUT,PATCH,DELETE,OPTIONS"))
					Expect(res.Header.Get("Access-Control-Allow-Headers")).To(Equal("Accept,Authorization,Content-Type"))
					Expect(res.Header.Get("Access-Control-Allow-Credentials")).To(Equal("true"))
				})
			})

			Context("when a regular request is made", func() {
				It("responds with allow-origin header", func() {
					res, err = http.Get(s.URL + "/")
					Expect(err).To(BeNil())

					Expect(res.Header.Get("Access-Control-Allow-Origin")).To(Equal("*"))
				})
			})
		})
	})
})
