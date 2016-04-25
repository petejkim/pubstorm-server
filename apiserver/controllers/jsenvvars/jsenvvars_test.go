package jsenvvars_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"
	"github.com/streadway/amqp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "jsenvvars")
}

var _ = Describe("JSEnvVars", func() {
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
		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.DeleteQueue(mq, queues.All...)

		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
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

	Describe("PUT /projects/:project_name/jsenvvars/add", func() {
		var (
			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			params = make(map[string]string)
			depl   *deployment.Deployment
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			params["foo"] = "bar"

			now := time.Now()
			depl = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				State:      deployment.StateDeployed,
				DeployedAt: &now,
			})
			db.Model(proj).UpdateColumn("active_deployment_id", depl.ID)
		})

		AfterEach(func() {
			s3client.S3 = origS3
		})

		doRequestWith := func(b []byte) {
			s = httptest.NewServer(server.New())

			req, err := http.NewRequest("PUT", s.URL+"/projects/foo-bar-express/jsenvvars/add", bytes.NewBuffer(b))
			Expect(err).To(BeNil())
			req.Header.Add("Content-Type", "application/json")

			if headers != nil {
				for k, v := range headers {
					for _, h := range v {
						req.Header.Add(k, h)
					}
				}
			}

			res, err = http.DefaultClient.Do(req)
			Expect(err).To(BeNil())
		}

		doRequest := func() {
			b, err := json.Marshal(params)
			Expect(err).To(BeNil())

			doRequestWith(b)
		}

		assertNoDeployment := func() {
			// Don't enqueue any messages to deployment queue
			Expect(testhelper.ConsumeQueue(mq, queues.Deploy)).To(BeNil())
			// Don't copy a bundle from current deployment
			Expect(fakeS3.CopyCalls.Count()).To(Equal(0))
			var count int
			Expect(db.Model(deployment.Deployment{}).Where("id <> ?", depl.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(0))
		}

		Context("when active_deployment_id exists", func() {
			var newDepl *deployment.Deployment

			BeforeEach(func() {
				doRequest()

				newDepl = &deployment.Deployment{}
				db.Last(newDepl)
			})

			It("return 202 with accepted", func() {
				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				j := map[string]interface{}{
					"deployment": map[string]interface{}{
						"id":      newDepl.ID,
						"state":   deployment.StatePendingDeploy,
						"version": newDepl.Version,
					},
				}

				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))

				Expect(newDepl.JsEnvVars).To(MatchJSON(`{"foo": "bar"}`))
			})

			It("uploads bundle to s3", func() {
				Expect(fakeS3.CopyCalls.Count()).To(Equal(1))
				call := fakeS3.CopyCalls.NthCall(1)
				Expect(call).NotTo(BeNil())
				Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(call.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)))
				Expect(call.Arguments[3]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", newDepl.Prefix, newDepl.ID)))
			})

			It("enqueues a deploy job", func() {
				d := testhelper.ConsumeQueue(mq, queues.Deploy)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": false,
						"skip_invalidation": false,
						"use_raw_bundle": false
					}
				`, newDepl.ID)))
			})

			It("marks the deployment as 'pending_deploy'", func() {
				doRequest()

				Expect(newDepl.State).To(Equal(deployment.StatePendingDeploy))
			})
		})

		Context("when there is no changes", func() {
			BeforeEach(func() {
				Expect(db.Model(depl).UpdateColumn("js_env_vars", `{"foo": "bar"}`).Error).To(BeNil())
				doRequest()
			})

			It("return 202 with accepted", func() {
				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(db.First(depl, depl.ID).Error).To(BeNil())
				j := map[string]interface{}{
					"deployment": map[string]interface{}{
						"id":          depl.ID,
						"state":       depl.State,
						"version":     depl.Version,
						"deployed_at": depl.DeployedAt,
					},
				}

				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))

				assertNoDeployment()
			})
		})

		DescribeTable("errors",
			func(setup func(), expectedCode int, expectedBody string) {
				setup()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(expectedCode))
				Expect(b.String()).To(MatchJSON(expectedBody))

				assertNoDeployment()
			},
			Entry("when there is no active deployment", func() {
				db.Model(proj).UpdateColumn("active_deployment_id", nil)
				doRequest()
			}, http.StatusPreconditionFailed, `{
				"error":             "precondition_failed",
				"error_description": "current active deployment could not be found"
			}`),
			Entry("when request body is invalid json", func() {
				doRequestWith([]byte(`{hello`))
			}, http.StatusBadRequest, `{
				"error": "invalid_request",
				"error_description": "request body is in invalid format"
			}`),
			Entry("when request body is empty", func() {
				doRequestWith([]byte(`{}`))
			}, 422, `{
				"error": "invalid_params",
				"error_description": "request body is empty"
			}`),
		)

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, func() {
			assertNoDeployment()
		})

		sharedexamples.ItRequiresProjectCollab(func() (*gorm.DB, *user.User, *project.Project) {
			return db, u, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, func() {
			assertNoDeployment()
		})

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, func() {
			assertNoDeployment()
		})
	})
})
