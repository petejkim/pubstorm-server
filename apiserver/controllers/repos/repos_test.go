package repos_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
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
	RunSpecs(t, "repos")
}

var _ = Describe("Repos", func() {
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

	Describe("GET /projects/:project_name/repos", func() {
		var (
			headers http.Header

			u    *user.User
			t    *oauthtoken.OauthToken
			proj *project.Project
			rp   *repo.Repo
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			rp = &repo.Repo{
				ProjectID:   proj.ID,
				UserID:      u.ID,
				URI:         "git@github.com:golang/talks.git",
				Branch:      "release",
				WebhookPath: "ABCDE4242",
			}
			Expect(db.Create(rp).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/"+proj.Name+"/repos", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("responds with HTTP OK and the repository information", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"repo": {
					"project_id": %d,
					"uri": "git@github.com:golang/talks.git",
					"branch": "release",
					"webhook_url": "%s",
					"webhook_secret": "%s"
				}
			}`, proj.ID, fmt.Sprintf("%s/hooks/github/%s", common.WebhookHost, rp.WebhookPath), rp.WebhookSecret)))
		})

		Context("when project is not linked to a repository", func() {
			BeforeEach(func() {
				err := db.Delete(rp).Error
				Expect(err).To(BeNil())
			})

			It("responds with HTTP 404 Not Found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "project not linked to any repository"
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
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

	Describe("POST /projects/:project_name/repos", func() {
		var (
			headers http.Header
			params  url.Values

			u    *user.User
			t    *oauthtoken.OauthToken
			proj *project.Project
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			params = url.Values{
				"uri":    {"git@github.com:golang/talks.git"},
				"branch": {"release"},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/"+proj.Name+"/repos", params, headers, nil)
			Expect(err).To(BeNil())
		}

		It("creates a repo record and responds with HTTP 201 Created", func() {
			doRequest()

			rp := &repo.Repo{}
			err := db.Where("project_id = ?", proj.ID).First(&rp).Error
			Expect(err).To(BeNil())

			Expect(rp.ProjectID).To(Equal(proj.ID))
			Expect(rp.URI).To(Equal("git@github.com:golang/talks.git"))
			Expect(rp.Branch).To(Equal("release"))
			Expect(rp.WebhookPath).NotTo(BeEmpty())
			Expect(rp.WebhookSecret).To(Equal(""))

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusCreated))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"repo": {
					"project_id": %d,
					"uri": "git@github.com:golang/talks.git",
					"branch": "release",
					"webhook_url": "%s",
					"webhook_secret": "%s"
				}
			}`, proj.ID, fmt.Sprintf("%s/hooks/github/%s", common.WebhookHost, rp.WebhookPath), rp.WebhookSecret)))
		})

		Context("when repo branch is not specified", func() {
			BeforeEach(func() {
				params.Set("branch", "")
			})

			It("defaults to the 'master' branch", func() {
				doRequest()

				rp := &repo.Repo{}
				err := db.Where("project_id = ?", proj.ID).First(&rp).Error
				Expect(err).To(BeNil())

				Expect(rp.Branch).To(Equal("master"))
			})
		})

		Context("when webhook secret is specified", func() {
			BeforeEach(func() {
				params.Add("secret", "my little secret pony")
			})

			It("saves the secret but does not render it in the response", func() {
				doRequest()

				rp := &repo.Repo{}
				err := db.Where("project_id = ?", proj.ID).First(&rp).Error
				Expect(err).To(BeNil())

				Expect(rp.WebhookSecret).To(Equal("my little secret pony"))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusCreated))
				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"repo": {
						"project_id": %d,
						"uri": "git@github.com:golang/talks.git",
						"branch": "release",
						"webhook_url": "%s",
						"webhook_secret": "my little secret pony"
					}
			}`, proj.ID, fmt.Sprintf("%s/hooks/github/%s", common.WebhookHost, rp.WebhookPath))))
			})
		})

		Context("when uri param is empty", func() {
			BeforeEach(func() {
				params.Set("uri", "")
			})

			It("responds with HTTP 422 with invalid_params", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"errors": {
							"uri": "is required"
						}
					}`))
			})
		})

		Context("when project is already linked to a repo", func() {
			BeforeEach(func() {
				rp := &repo.Repo{
					ProjectID: proj.ID,
					UserID:    u.ID,
					URI:       "https://github.com/PubStorm/pubstorm-www.git",
					Branch:    "master",
				}
				Expect(db.Create(rp).Error).To(BeNil())
			})

			It("responds with HTTP 409 Conflict", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusConflict))
				Expect(b.String()).To(MatchJSON(`{
						"error": "already_exists",
						"error_description": "project already linked to a repository"
					}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
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

	Describe("DELETE /projects/:project_name/repos", func() {
		var (
			headers http.Header

			u    *user.User
			t    *oauthtoken.OauthToken
			proj *project.Project
			rp   *repo.Repo
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			rp = &repo.Repo{
				ProjectID:   proj.ID,
				UserID:      u.ID,
				URI:         "git@github.com:golang/talks.git",
				Branch:      "release",
				WebhookPath: "ABCDE4242",
			}
			Expect(db.Create(rp).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE", s.URL+"/projects/"+proj.Name+"/repos", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("deletes the repo record and responds with HTTP OK", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(`{ "deleted": true }`))
		})

		Context("when project is not linked to a repository", func() {
			BeforeEach(func() {
				err := db.Delete(rp).Error
				Expect(err).To(BeNil())
			})

			It("responds with HTTP 404 Not Found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "project not linked to any repository"
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
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
