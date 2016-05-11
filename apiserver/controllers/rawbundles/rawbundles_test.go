package rawbundles_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
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
	RunSpecs(t, "raw_bundles")
}

var _ = Describe("RawBundles", func() {
	var (
		db  *gorm.DB
		s   *httptest.Server
		res *http.Response
		err error

		u  *user.User
		oc *oauthclient.OauthClient
		t  *oauthtoken.OauthToken
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u, oc, t = factories.AuthTrio(db)
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("GET /projects/:project_name/raw_bundles/:bundle_checksum", func() {
		var (
			proj *project.Project
			bun  *rawbundle.RawBundle

			headers http.Header
		)

		BeforeEach(func() {
			proj = factories.Project(db, u)
			bun = &rawbundle.RawBundle{
				ProjectID:    proj.ID,
				Checksum:     "ch3ck5um",
				UploadedPath: "foo/bar",
			}
			Expect(db.Create(bun).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/"+proj.Name+"/raw_bundles/"+bun.Checksum, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns 200 OK and raw bundle json", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"raw_bundle": {
					"id": %d,
					"checksum": "%s",
					"uploaded_path": "%s"
				}
			}`, bun.ID, bun.Checksum, bun.UploadedPath)))
		})

		Context("when the bundle does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(bun).Error).To(BeNil())
			})

			It("returns 404 not found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "raw bundle could not be found"
				}`))
			})
		})

		Context("when the bundle does not associated to the project", func() {
			BeforeEach(func() {
				proj2 := factories.Project(db, u)
				bun.ProjectID = proj2.ID
				Expect(db.Save(bun).Error).To(BeNil())
			})

			It("returns 404 not found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "raw bundle could not be found"
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProjectCollab(func() (*gorm.DB, *user.User, *project.Project) {
			return db, u, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
