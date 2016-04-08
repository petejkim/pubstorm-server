package domains_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"
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
		fakeS3 *fake.S3
		origS3 filetransfer.FileTransfer

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
		origS3 = s3client.S3
		fakeS3 = &fake.S3{}
		s3client.S3 = fakeS3

		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)
		testhelper.DeleteExchange(mq, exchanges.All...)

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
		s3client.S3 = origS3

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
						"` + proj.DefaultDomainName() + `"
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
						"` + proj.DefaultDomainName() + `",
						"www.foo-bar-express.com",
						"www.foobarexpress.com"
					]
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
					origMaxDomains = shared.MaxDomainsPerProject
					shared.MaxDomainsPerProject = 2

					for i := 0; i < shared.MaxDomainsPerProject; i++ {
						factories.Domain(db, proj)
					}

					doRequest()
				})

				AfterEach(func() {
					shared.MaxDomainsPerProject = origMaxDomains
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

					Expect(domainCount).To(Equal(shared.MaxDomainsPerProject))
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

				Context("when an apex domain is given", func() {
					BeforeEach(func() {
						params.Set("name", "foo-bar-express.com")
					})

					It("prepends www. prefix to the domain name given", func() {
						b := &bytes.Buffer{}
						_, err := b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(http.StatusCreated))
						Expect(b.String()).To(MatchJSON(`{
							"domain": {
								"name": "www.foo-bar-express.com"
							}
						}`))

						Expect(dom.Name).To(Equal("www.foo-bar-express.com"))
						Expect(dom.ProjectID).To(Equal(proj.ID))
					})
				})
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

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
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
			qName      string // invalidation queue
		)

		BeforeEach(func() {
			d = factories.Domain(db, proj)
			domainName = d.Name
			qName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE", s.URL+"/projects/foo-bar-express/domains/"+domainName, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when a given domain does not exist", func() {
			BeforeEach(func() {
				domainName += "xx"
			})

			It("returns 404 error", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))

				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "domain could not be found"
				}`))
			})
		})

		Context("when a given domain exists", func() {
			It("deletes the domain from the project", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"deleted": true
				}`))

				var count int
				err = db.Model(domain.Domain{}).Where("id = ?", d.ID).Count(&count).Error
				Expect(err).To(BeNil())
				Expect(count).To(BeZero())
			})

			It("deletes the meta.json for the domain from s3", func() {
				doRequest()

				Expect(fakeS3.DeleteCalls.Count()).To(Equal(1))

				deleteCall := fakeS3.DeleteCalls.NthCall(1)
				Expect(deleteCall).NotTo(BeNil())
				Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(deleteCall.Arguments[2]).To(Equal("/domains/" + domainName + "/meta.json"))
				Expect(deleteCall.ReturnValues[0]).To(BeNil())
			})

			It("publishes invalidation message for the domain", func() {
				doRequest()

				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
					"domains": ["%s"]
				}`, domainName)))
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

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
