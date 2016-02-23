package ping_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/petejkim/rise-server/server"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ping")
}

var _ = Describe("Ping", func() {
	var (
		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		s = httptest.NewServer(server.New())
		res, err = http.Get(s.URL + "/ping")
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	It("returns 200 OK and a json message containing pong", func() {
		var j map[string]string
		err = json.NewDecoder(res.Body).Decode(&j)
		Expect(err).To(BeNil())

		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(j["message"]).To(Equal("pong"))
	})
})
