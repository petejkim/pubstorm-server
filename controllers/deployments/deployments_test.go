package deployments_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/common"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/deployment"
	"github.com/nitrous-io/rise-server/models/oauthclient"
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/project"
	"github.com/nitrous-io/rise-server/models/user"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/queues"
	"github.com/nitrous-io/rise-server/server"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/shared"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "deployments")
}

var _ = Describe("Deployments", func() {
	var (
		db *gorm.DB
		mq *amqp.Connection

		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("POST /projects/:name/deployments", func() {
		var (
			fakeS3 *fake.FileTransfer
			origS3 filetransfer.FileTransfer
			err    error

			u  *user.User
			oc *oauthclient.OauthClient
			t  *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
		)

		BeforeEach(func() {
			origS3 = common.S3
			fakeS3 = &fake.FileTransfer{}
			common.S3 = fakeS3

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
			common.S3 = origS3
		})

		doRequestWithMultipart := func(partName string) {
			s = httptest.NewServer(server.New())

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			part, err := writer.CreateFormFile(partName, "/tmp/rise/foo.tar.gz")
			Expect(err).To(BeNil())

			_, err = part.Write([]byte("hello\nworld!"))
			Expect(err).To(BeNil())

			err = writer.Close()
			Expect(err).To(BeNil())

			req, err := http.NewRequest("POST", s.URL+"/projects/foo-bar-express/deployments", body)
			Expect(err).To(BeNil())

			req.Header.Set("Content-Type", writer.FormDataContentType())
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
			doRequestWithMultipart("payload")
		}

		doRequestWithWrongPart := func() {
			doRequestWithMultipart("upload")
		}

		doRequestWithoutMultipart := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/deployments", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		shared.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		})

		Context("when the project does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(proj).Error).To(BeNil())
				doRequest()
			})

			It("returns 404 not found and does not upload", func() {
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found"
				}`))
				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

				depl := &deployment.Deployment{}
				Expect(db.Last(&depl).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the project does not belong to current user", func() {
			BeforeEach(func() {
				u2 := factories.User(db)
				err = db.Model(&proj).UpdateColumn("user_id", u2.ID).Error
				Expect(err).To(BeNil())

				doRequest()
			})

			It("returns 404 not found and does not upload", func() {
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found"
				}`))
				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

				depl := &deployment.Deployment{}
				Expect(db.Last(&depl).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the project belongs to current user", func() {
			Context("when the request is not multipart", func() {
				It("returns 400 with invalid_request", func() {
					doRequestWithoutMultipart()

					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "the request should be encoded in multipart/form-data format"
					}`))
					Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

					depl := &deployment.Deployment{}
					Expect(db.Last(&depl).Error).To(Equal(gorm.RecordNotFound))
				})
			})

			Context("when the request does not contain payload part", func() {
				It("returns 422 with invalid_params", func() {
					doRequestWithWrongPart()

					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"errors": {
							"payload": "is required"
						}
					}`))
					Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

					depl := &deployment.Deployment{}
					Expect(db.Last(&depl).Error).To(Equal(gorm.RecordNotFound))
				})
			})

			Context("when the payload is larger than the limit", func() {
				var origMaxUploadSize int64

				BeforeEach(func() {
					origMaxUploadSize = common.MaxUploadSize
					common.MaxUploadSize = 10
					doRequest()
				})

				AfterEach(func() {
					common.MaxUploadSize = origMaxUploadSize
				})

				It("returns 400 with invalid_request", func() {
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "request body is too large"
					}`))
					Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

					depl := &deployment.Deployment{}
					Expect(db.Last(&depl).Error).To(Equal(gorm.RecordNotFound))
				})
			})

			Context("when the request is valid", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					doRequest()
					depl = &deployment.Deployment{}
					db.Last(depl)
				})

				It("returns 202 accepted", func() {
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(res.StatusCode).To(Equal(http.StatusAccepted))
					Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
						"deployment": {
							"id": %d,
							"state": "%s"
						}
					}`, depl.ID, deployment.StatePendingDeploy)))
				})

				It("creates a deployment record", func() {
					Expect(depl).NotTo(BeNil())
					Expect(depl.ProjectID).To(Equal(proj.ID))
					Expect(depl.UserID).To(Equal(u.ID))
					Expect(depl.State).To(Equal(deployment.StatePendingDeploy))
					Expect(depl.Prefix).NotTo(HaveLen(0))
				})

				It("uploads bundle to s3", func() {
					Expect(fakeS3.UploadCalls.Count()).To(Equal(1))
					call := fakeS3.UploadCalls.NthCall(1)
					Expect(call).NotTo(BeNil())
					Expect(call.Arguments[0]).To(Equal(common.S3BucketRegion))
					Expect(call.Arguments[1]).To(Equal(common.S3BucketName))
					Expect(call.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)))
					Expect(call.Arguments[4]).To(Equal("private"))
					Expect(call.SideEffects["uploaded_content"]).To(Equal([]byte("hello\nworld!")))
				})

				It("enqueues a deploy job", func() {
					d := testhelper.ConsumeQueue(mq, queues.Deploy)
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
						{
							"deployment_id": %d,
							"deployment_prefix": "%s",
							"project_name": "%s",
							"domain": "%s.rise.cloud"
						}
					`, depl.ID, depl.Prefix, proj.Name, proj.Name)))
				})
			})
		})
	})
})
