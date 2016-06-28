package acme_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "acme")
}

var _ = Describe("Acme", func() {
	var (
		db *gorm.DB

		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("GET /.well-known/acme-challenge/:token", func() {
		var acmeCert *acmecert.AcmeCert

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/.well-known/acme-challenge/secrud-token", nil, nil, nil)
			Expect(err).To(BeNil())
		}

		BeforeEach(func() {
			var err error

			u := factories.User(db)

			proj := &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			dm := factories.Domain(db, proj, "www.foo-bar-express.com")

			aesKey := "something-something-something-32"
			acmeCert, err = acmecert.New(dm.ID, aesKey)
			Expect(err).To(BeNil())
			acmeCert.HTTPChallengePath = "/.well-known/acme-challenge/secrud-token"
			acmeCert.HTTPChallengeResource = "secrud-token.abcde12345"
			Expect(db.Create(acmeCert).Error).To(BeNil())
		})

		It("responds with the challenge resource corresponding to the path", func() {
			doRequest()

			Expect(res.StatusCode).To(Equal(http.StatusOK))

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(b.String()).To(Equal(acmeCert.HTTPChallengeResource))
		})

		Context("when no challenge with the token exists", func() {
			BeforeEach(func() {
				err := db.Delete(acmeCert).Error
				Expect(err).To(BeNil())
			})

			It("responds with HTTP 404", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	})
})
