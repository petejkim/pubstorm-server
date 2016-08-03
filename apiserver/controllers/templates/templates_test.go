package templates_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/template"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "templates")
}

var _ = Describe("Templates", func() {
	var (
		db  *gorm.DB
		s   *httptest.Server
		res *http.Response
		err error

		u *user.User
		t *oauthtoken.OauthToken
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u, _, t = factories.AuthTrio(db)
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("GET /templates", func() {
		var (
			headers http.Header

			tmpl1 *template.Template
			tmpl2 *template.Template
			tmpl3 *template.Template
		)

		BeforeEach(func() {
			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			tmpl1 = factories.Template(db, 1, "Default")
			tmpl2 = factories.Template(db, 2, "Minimalist")
			tmpl3 = factories.Template(db, 3, "Store")
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/templates", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns templates ordered by rank", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"templates": [
					{
						"id": %d,
						"name": "%s",
						"rank": 1,
						"download_url": "%s",
						"preview_url": "%s",
						"preview_image_url": "%s"
					},
					{
						"id": %d,
						"name": "%s",
						"rank": 2,
						"download_url": "%s",
						"preview_url": "%s",
						"preview_image_url": "%s"
					},
					{
						"id": %d,
						"name": "%s",
						"rank": 3,
						"download_url": "%s",
						"preview_url": "%s",
						"preview_image_url": "%s"
					}
				]
			}`,
				tmpl1.ID, tmpl1.Name, tmpl1.DownloadURL, tmpl1.PreviewURL, tmpl1.PreviewImageURL,
				tmpl2.ID, tmpl2.Name, tmpl2.DownloadURL, tmpl2.PreviewURL, tmpl2.PreviewImageURL,
				tmpl3.ID, tmpl3.Name, tmpl3.DownloadURL, tmpl3.PreviewURL, tmpl3.PreviewImageURL)))
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
