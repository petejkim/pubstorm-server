package projects_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
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
	"github.com/streadway/amqp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "projects")
}

var _ = Describe("Projects", func() {
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

	Describe("POST /projects", func() {
		var (
			params  url.Values
			headers http.Header
		)

		BeforeEach(func() {
			params = url.Values{
				"name": {"foo-bar-express"},
			}
			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects", params, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when the project name is empty", func() {
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

		Context("when the project name is invalid", func() {
			BeforeEach(func() {
				params.Set("name", "foo-bar-")
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

		Context("when the project name is taken", func() {
			BeforeEach(func() {
				proj2 := &project.Project{
					Name:   "foo-bar-express",
					UserID: u.ID,
				}

				err := db.Create(proj2).Error
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

		Context("when the project name is blacklisted", func() {
			BeforeEach(func() {
				factories.BlacklistedName(db, "foo-bar-express")
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

		Context("when a valid project name is given", func() {
			var proj *project.Project

			BeforeEach(func() {
				doRequest()
				proj = &project.Project{}
				err := db.Last(proj).Error
				Expect(err).To(BeNil())
			})

			It("returns 201 created", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusCreated))
				Expect(b.String()).To(MatchJSON(`{
					"project": {
						"name": "foo-bar-express"
					}
				}`))
			})

			It("creates a project record in the DB", func() {
				Expect(proj.Name).To(Equal("foo-bar-express"))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("GET /projects/:projectName", func() {
		var (
			proj *project.Project

			headers http.Header
		)

		BeforeEach(func() {
			proj = factories.Project(db, u)
			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/"+proj.Name, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns 200 OK and project json", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"project": {
					"name": "%s"
				}
			}`, proj.Name)))
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

	Describe("GET /projects", func() {
		var (
			params  url.Values
			headers http.Header

			anotherU *user.User
			proj     *project.Project
			proj2    *project.Project
			proj3    *project.Project
		)

		BeforeEach(func() {
			params = url.Values{
				"name": {"foo-bar-express"},
			}
			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			anotherU = factories.User(db)

			proj = factories.Project(db, u)
			proj2 = factories.Project(db, anotherU)
			proj3 = factories.Project(db, u)
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns current user's projects ordered by name", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"projects": [
					{
						"name": "%s"
					},
					{
						"name": "%s"
					}
				]
			}`, proj.Name, proj3.Name)))
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("DELETE /projects/:name", func() {
		var (
			fakeS3                *fake.S3
			origS3                filetransfer.FileTransfer
			mq                    *amqp.Connection
			invalidationQueueName string

			proj *project.Project
			dm1  *domain.Domain
			dm2  *domain.Domain

			proj2   *project.Project
			dm3     *domain.Domain
			headers http.Header
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			testhelper.DeleteQueue(mq, queues.All...)
			testhelper.DeleteExchange(mq, exchanges.All...)

			invalidationQueueName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			proj = factories.Project(db, u)
			dm1 = factories.Domain(db, proj)
			dm2 = factories.Domain(db, proj)

			proj2 = factories.Project(db, u)
			dm3 = factories.Domain(db, proj2)
		})

		AfterEach(func() {
			s3client.S3 = origS3
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE", s.URL+"/projects/"+proj.Name, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns 200 with OK", func() {
			doRequest()
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(`{
				"deleted": true
			}`))
		})

		It("deletes associated domains", func() {
			doRequest()
			var dms []domain.Domain

			err := db.Where("project_id = ?", proj.ID).Find(&dms).Error
			Expect(err).To(BeNil())
			Expect(len(dms)).To(Equal(0))

			// Make sure it does not delete other project's domains
			Expect(db.First(&domain.Domain{}, dm3.ID).Error).To(BeNil())
		})

		It("deletes the given project", func() {
			doRequest()
			Expect(db.First(&project.Project{}, proj.ID).Error).To(Equal(gorm.RecordNotFound))
			// Make sure it does not delete other projects
			Expect(db.First(&project.Project{}, proj2.ID).Error).To(BeNil())
		})

		It("deletes the meta.json for the associated domains from s3", func() {
			doRequest()

			Expect(fakeS3.DeleteCalls.Count()).To(Equal(3))

			for i, domainName := range []string{proj.Name + "." + shared.DefaultDomain, dm1.Name, dm2.Name} {
				deleteCall := fakeS3.DeleteCalls.NthCall(i + 1)
				Expect(deleteCall).NotTo(BeNil())
				Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(deleteCall.Arguments[2]).To(Equal("/domains/" + domainName + "/meta.json"))
				Expect(deleteCall.ReturnValues[0]).To(BeNil())
			}
		})

		It("publishes invalidation message for the domain", func() {
			doRequest()

			d := testhelper.ConsumeQueue(mq, invalidationQueueName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"domains": ["%s", "%s", "%s"]
			}`, proj.Name+"."+shared.DefaultDomain, dm1.Name, dm2.Name)))
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

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
