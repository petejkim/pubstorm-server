package projects_test

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
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/pkg/tracker"
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

		fakeTracker *fake.Tracker
		origTracker tracker.Trackable
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u, oc, t = factories.AuthTrio(db)

		origTracker = common.Tracker
		fakeTracker = &fake.Tracker{}
		common.Tracker = fakeTracker
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()

		common.Tracker = origTracker
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

			It("tracks a 'Used Blacklisted Project Name' event", func() {
				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Used Blacklisted Project Name"))

				t := trackCall.Arguments[2]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal("foo-bar-express"))

				Expect(trackCall.Arguments[3]).To(BeNil())
				Expect(trackCall.ReturnValues[0]).To(BeNil())
			})
		})

		Context("when the project name contains uppercase characters", func() {
			BeforeEach(func() {
				params.Set("name", "Foo-Bar-Express")
				doRequest()
			})

			It("converts those characters to lowercase", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusCreated))
				Expect(b.String()).To(MatchJSON(`{
					"project": {
						"name": "foo-bar-express",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
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
						"name": "foo-bar-express",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
					}
				}`))
			})

			It("creates a project record in the DB", func() {
				Expect(proj.Name).To(Equal("foo-bar-express"))
			})

			It("tracks a 'Created Project' event", func() {
				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Created Project"))

				t := trackCall.Arguments[2]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal("foo-bar-express"))

				Expect(trackCall.Arguments[3]).To(BeNil())
				Expect(trackCall.ReturnValues[0]).To(BeNil())
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
					"name": "%s",
					"default_domain_enabled": true,
					"force_https": false,
					"skip_build": false
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
			headers http.Header

			anotherU *user.User
			proj     *project.Project
			proj2    *project.Project
			proj3    *project.Project
		)

		BeforeEach(func() {
			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			anotherU = factories.User(db)

			proj = factories.Project(db, u, "site-1")
			proj2 = factories.Project(db, anotherU, "site-2")
			proj3 = factories.Project(db, u, "site-3")
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
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
					},
					{
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
					}
				],
				"shared_projects": []
			}`, proj.Name, proj3.Name)))
		})

		Context("when user is a collaborator of other users' projects", func() {
			var (
				yetAnotherU *user.User
				proj4       *project.Project
				proj5       *project.Project
				proj6       *project.Project
			)

			BeforeEach(func() {
				yetAnotherU = factories.User(db)

				proj4 = factories.Project(db, anotherU, "site-4")
				proj5 = factories.Project(db, yetAnotherU, "site-5")
				proj6 = factories.Project(db, yetAnotherU, "site-6")

				err := proj4.AddCollaborator(db, u)
				Expect(err).To(BeNil())
				err = proj5.AddCollaborator(db, u)
				Expect(err).To(BeNil())
			})

			It("returns the shared projects ordered by name", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"projects": [
						{
							"name": "%s",
							"default_domain_enabled": true,
							"force_https": false,
							"skip_build": false
						},
						{
							"name": "%s",
							"default_domain_enabled": true,
							"force_https": false,
							"skip_build": false
						}
					],
					"shared_projects": [
						{
							"name": "%s",
							"default_domain_enabled": true,
							"force_https": false,
							"skip_build": false
						},
						{
							"name": "%s",
							"default_domain_enabled": true,
							"force_https": false,
							"skip_build": false
						}
					]
				}`, proj.Name, proj3.Name, proj4.Name, proj5.Name)))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("PUT /projects/:name", func() {
		var (
			fakeS3                *fake.S3
			origS3                filetransfer.FileTransfer
			mq                    *amqp.Connection
			invalidationQueueName string

			proj *project.Project

			params  url.Values
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
		})

		AfterEach(func() {
			s3client.S3 = origS3
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("PUT", s.URL+"/projects/"+proj.Name, params, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when default domain is newly disabled (i.e. it was enabled)", func() {
			BeforeEach(func() {
				Expect(proj.DefaultDomainEnabled).To(Equal(true))
				params = url.Values{
					"default_domain_enabled": {"false"},
				}
			})

			It("returns 200 OK", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())
				Expect(proj.DefaultDomainEnabled).To(Equal(false))

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"project":{
						"name": "%s",
						"default_domain_enabled": false,
						"force_https": false,
						"skip_build": false
					}
				}`, proj.Name)))
			})

			Context("when there is an active deployment", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					depl = factories.Deployment(db, proj, u, deployment.StateDeployed)
					err := db.Model(proj).Update("active_deployment_id", depl.ID).Error
					Expect(err).To(BeNil())
				})

				It("deletes the meta.json for the default domain from S3", func() {
					doRequest()

					Expect(fakeS3.DeleteCalls.Count()).To(Equal(1))

					deleteCall := fakeS3.DeleteCalls.NthCall(1)
					Expect(deleteCall).NotTo(BeNil())
					Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
					Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
					Expect(deleteCall.Arguments[2]).To(Equal("/domains/" + proj.Name + "." + shared.DefaultDomain + "/meta.json"))
					Expect(deleteCall.ReturnValues[0]).To(BeNil())
				})

				It("publishes invalidation message for the default domain", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, invalidationQueueName)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
						"domains": ["%s"]
					}`, proj.Name+"."+shared.DefaultDomain)))
				})
			})

			Context("when there is no active deployment", func() {
				It("does not delete the meta.json for the default domain from S3", func() {
					doRequest()

					Expect(fakeS3.DeleteCalls.Count()).To(Equal(0))
				})

				It("does not enqueue any job", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d).To(BeNil())
				})
			})
		})

		Context("when default domain is newly enabled (i.e. it was disabled)", func() {
			BeforeEach(func() {
				proj.DefaultDomainEnabled = false
				Expect(db.Save(proj).Error).To(BeNil())
				params = url.Values{
					"default_domain_enabled": {"true"},
				}
			})

			It("returns 200 OK", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())
				Expect(proj.DefaultDomainEnabled).To(Equal(true))

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"project":{
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
					}
				}`, proj.Name)))
			})

			Context("when there is an active deployment", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					depl = factories.Deployment(db, proj, u, deployment.StateDeployed)
					err := db.Model(proj).Update("active_deployment_id", depl.ID).Error
					Expect(err).To(BeNil())
				})

				It("enqueues a deploy job to upload meta.json", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": true,
						"use_raw_bundle": false
					}`, *proj.ActiveDeploymentID)))
				})
			})

			Context("when there is no active deployment", func() {
				It("does not enqueue any job", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d).To(BeNil())
				})
			})
		})

		Context("when force_https is newly enabled (i.e. it was disabled)", func() {
			BeforeEach(func() {
				proj.ForceHTTPS = false
				Expect(db.Save(proj).Error).To(BeNil())
				params = url.Values{
					"force_https": {"true"},
				}
			})

			It("returns 200 OK", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())
				Expect(proj.DefaultDomainEnabled).To(Equal(true))

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"project":{
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": true,
						"skip_build": false
					}
				}`, proj.Name)))
			})

			Context("when there is an active deployment", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					depl = factories.Deployment(db, proj, u, deployment.StateDeployed)
					err := db.Model(proj).Update("active_deployment_id", depl.ID).Error
					Expect(err).To(BeNil())
				})

				It("enqueues a deploy job to update meta.json", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": false,
						"use_raw_bundle": false
					}`, *proj.ActiveDeploymentID)))
				})
			})
		})

		Context("when force_https is newly disabled (i.e. it was enabled)", func() {
			BeforeEach(func() {
				proj.ForceHTTPS = true
				Expect(db.Save(proj).Error).To(BeNil())
				params = url.Values{
					"force_https": {"false"},
				}
			})

			It("returns 200 OK", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())
				Expect(proj.DefaultDomainEnabled).To(Equal(true))

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"project":{
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": false
					}
				}`, proj.Name)))
			})

			Context("when there is an active deployment", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					depl = factories.Deployment(db, proj, u, deployment.StateDeployed)
					err := db.Model(proj).Update("active_deployment_id", depl.ID).Error
					Expect(err).To(BeNil())
				})

				It("enqueues a deploy job to update meta.json", func() {
					doRequest()

					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": false,
						"use_raw_bundle": false
					}`, *proj.ActiveDeploymentID)))
				})
			})
		})

		Context("when skip_build set to true", func() {
			BeforeEach(func() {
				proj.SkipBuild = false
				Expect(db.Save(proj).Error).To(BeNil())
				params = url.Values{
					"skip_build": {"true"},
				}
			})

			It("returns 200 OK", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())
				Expect(proj.DefaultDomainEnabled).To(Equal(true))

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"project":{
						"name": "%s",
						"default_domain_enabled": true,
						"force_https": false,
						"skip_build": true
					}
				}`, proj.Name)))
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

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
			return db, proj
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

			ct1 := &cert.Cert{
				DomainID:        dm1.ID,
				CertificatePath: "old/path",
				PrivateKeyPath:  "old/path",
			}
			Expect(db.Create(ct1).Error).To(BeNil())

			ct2 := &cert.Cert{
				DomainID:        dm2.ID,
				CertificatePath: "old/path",
				PrivateKeyPath:  "old/path",
			}
			Expect(db.Create(ct2).Error).To(BeNil())
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

		It("deletes associated domains and certs", func() {
			doRequest()

			var count int
			Expect(db.Model(domain.Domain{}).Where("project_id = ?", proj.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(0))

			Expect(db.Model(cert.Cert{}).Where("domain_id IN (?,?)", dm1.ID, dm2.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(0))
		})

		It("deletes meta.json and ssl certs for the associated domains from s3", func() {
			doRequest()

			Expect(fakeS3.DeleteCalls.Count()).To(Equal(1))

			deleteCall := fakeS3.DeleteCalls.NthCall(1)
			Expect(deleteCall).NotTo(BeNil())
			Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(deleteCall.ReturnValues[0]).To(BeNil())

			filesToDelete := []string{
				"domains/" + proj.DefaultDomainName() + "/meta.json",
				"domains/" + dm1.Name + "/meta.json",
				"certs/" + dm1.Name + "/ssl.crt",
				"certs/" + dm1.Name + "/ssl.key",
				"domains/" + dm2.Name + "/meta.json",
				"certs/" + dm2.Name + "/ssl.crt",
				"certs/" + dm2.Name + "/ssl.key",
			}

			for i, path := range filesToDelete {
				Expect(deleteCall.Arguments[2+i]).To(Equal(path))
			}
		})

		It("deletes the given project", func() {
			doRequest()
			Expect(db.First(&project.Project{}, proj.ID).Error).To(Equal(gorm.RecordNotFound))
		})

		It("publishes invalidation message for the associated domains", func() {
			doRequest()

			d := testhelper.ConsumeQueue(mq, invalidationQueueName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"domains": ["%s", "%s", "%s"]
			}`, proj.Name+"."+shared.DefaultDomain, dm1.Name, dm2.Name)))
		})

		It("tracks a 'Deleted Project' event", func() {
			doRequest()

			trackCall := fakeTracker.TrackCalls.NthCall(1)
			Expect(trackCall).NotTo(BeNil())
			Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
			Expect(trackCall.Arguments[1]).To(Equal("Deleted Project"))

			t := trackCall.Arguments[2]
			props, ok := t.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(props["projectName"]).To(Equal(proj.Name))

			Expect(trackCall.Arguments[3]).To(BeNil())
			Expect(trackCall.ReturnValues[0]).To(BeNil())
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
