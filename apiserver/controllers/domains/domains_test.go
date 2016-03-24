package domains_test

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
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/shared"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "domains")
}

var _ = Describe("Domains", func() {
	var (
		db *gorm.DB
		mq *amqp.Connection

		s   *httptest.Server
		res *http.Response
		err error

		u  *user.User
		oc *oauthclient.OauthClient
		t  *oauthtoken.OauthToken

		headers http.Header
		proj    *project.Project
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)

		u, oc, t = factories.AuthTrio(db)

		proj = &project.Project{
			Name:   "foo-bar-express",
			UserID: u.ID,
		}
		Expect(db.Create(proj).Error).To(BeNil())

		headers = http.Header{
			"Authorization": {"Bearer " + t.Token},
		}
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("GET /projects/:name/domains", func() {

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/foo-bar-express/domains", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when no custom domain is added", func() {
			It("lists only the default subdomain", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"domains": [
						"foo-bar-express.` + common.DefaultDomain + `"
					]
				}`))
			})
		})

		Context("when custom domains for this project exist", func() {
			BeforeEach(func() {
				for _, dn := range []string{"www.foo-bar-express.com", "www.foobarexpress.com"} {
					dom := &domain.Domain{
						Name:      dn,
						ProjectID: proj.ID,
					}

					err := db.Create(dom).Error
					Expect(err).To(BeNil())
				}
				doRequest()
			})

			It("lists all domains for the project", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"domains": [
						"foo-bar-express.` + common.DefaultDomain + `",
						"www.foo-bar-express.com",
						"www.foobarexpress.com"
					]
				}`))
			})
		})

		shared.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		shared.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("POST /projects/:name/domains", func() {
		var params url.Values

		BeforeEach(func() {
			params = url.Values{
				"name": {"www.foo-bar-express.com"},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/domains", params, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when the project belongs to current user", func() {
			Context("when the domain name is empty", func() {
				BeforeEach(func() {
					params.Del("name")
					doRequest()
				})

				It("returns 422 unprocessable entity", func() {
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"errors": {
							"name": "is required"
						}
					}`))
				})
			})

			Context("when the domain name is invalid", func() {
				BeforeEach(func() {
					params.Set("name", "www.foo-b@r-express.com")
					doRequest()
				})

				It("returns 422 unprocessable entity", func() {
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"errors": {
						"name": "is invalid"
					}
				}`))
				})
			})

			Context("when the domain name is taken", func() {
				BeforeEach(func() {
					dom := &domain.Domain{
						Name:      "www.foo-bar-express.com",
						ProjectID: proj.ID,
					}
					err := db.Create(dom).Error
					Expect(err).To(BeNil())

					doRequest()
				})

				It("returns 422 unprocessable entity", func() {
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"errors": {
							"name": "is taken"
						}
					}`))
				})
			})

			Context("when the project has reached max number of domains allowed", func() {
				var origMaxDomains int

				BeforeEach(func() {
					origMaxDomains = common.MaxDomainsPerProject
					common.MaxDomainsPerProject = 2

					for i := 0; i < common.MaxDomainsPerProject; i++ {
						factories.Domain(db, proj)
					}

					doRequest()
				})

				AfterEach(func() {
					common.MaxDomainsPerProject = origMaxDomains
				})

				It("returns 422 unprocessable entity", func() {
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "project cannot have more domains"
					}`))

					var domainCount int
					err = db.Model(domain.Domain{}).Where("project_id = ?", proj.ID).Count(&domainCount).Error
					Expect(err).To(BeNil())

					Expect(domainCount).To(Equal(common.MaxDomainsPerProject))
				})
			})

			Context("when a valid domain name is given", func() {
				var dom *domain.Domain

				JustBeforeEach(func() {
					doRequest()
					dom = &domain.Domain{}
					err := db.Last(dom).Error
					Expect(err).To(BeNil())
				})

				Context("when there is an active deployment", func() {
					var depl *deployment.Deployment

					BeforeEach(func() {
						depl = factories.Deployment(db, proj, u, deployment.StateDeployed)
						err := db.Model(proj).Update("active_deployment_id", depl.ID).Error
						Expect(err).To(BeNil())
					})

					It("returns 201 created", func() {
						b := &bytes.Buffer{}
						_, err := b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(http.StatusCreated))
						Expect(b.String()).To(MatchJSON(`{
							"domain": {
								"name": "www.foo-bar-express.com"
							}
						}`))
					})

					It("creates a domain record in the DB", func() {
						Expect(dom.Name).To(Equal("www.foo-bar-express.com"))
						Expect(dom.ProjectID).To(Equal(proj.ID))
					})

					It("enqueues a deploy job to upload meta.json", func() {
						d := testhelper.ConsumeQueue(mq, queues.Deploy)
						Expect(d).NotTo(BeNil())
						Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
							"deployment_id": %d,
							"skip_webroot_upload": true,
							"skip_invalidation": true
						}`, *proj.ActiveDeploymentID)))
					})
				})

				Context("when there is no active deployment", func() {
					It("returns 201 created", func() {
						b := &bytes.Buffer{}
						_, err := b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(http.StatusCreated))
						Expect(b.String()).To(MatchJSON(`{
							"domain": {
								"name": "www.foo-bar-express.com"
							}
						}`))
					})

					It("creates a domain record in the DB", func() {
						Expect(dom.Name).To(Equal("www.foo-bar-express.com"))
						Expect(dom.ProjectID).To(Equal(proj.ID))
					})

					It("does not enqueue any job", func() {
						d := testhelper.ConsumeQueue(mq, queues.Deploy)
						Expect(d).To(BeNil())
					})
				})
			})
		})

		shared.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		shared.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("DELETE /projects/:project_name/domains/:name", func() {
		var (
			domainName string
			d          *domain.Domain
		)

		BeforeEach(func() {
			d = factories.Domain(db, proj)
			domainName = d.Name
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE", s.URL+"/projects/foo-bar-express/domains/"+domainName, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("deletes the domain from the project", func() {
			doRequest()

			Expect(res.StatusCode).To(Equal(http.StatusOK))

			var count int
			err = db.Model(domain.Domain{}).Where("id = ?", d.ID).Count(&count).Error
			Expect(err).To(BeNil())
			Expect(count).To(BeZero())
		})

		shared.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		shared.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
