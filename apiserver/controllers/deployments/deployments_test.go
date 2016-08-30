package deployments_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/apiserver/models/template"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/pkg/tracker"
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
	RunSpecs(t, "deployments")
}

var _ = Describe("Deployments", func() {
	var (
		db *gorm.DB
		mq *amqp.Connection

		s   *httptest.Server
		res *http.Response
		err error

		fakeTracker *fake.Tracker
		origTracker tracker.Trackable
	)

	timeAgo := func(ago time.Duration) *time.Time {
		currentTime := time.Now()
		result := currentTime.Add(-ago)
		return &result
	}

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		origTracker = common.Tracker
		fakeTracker = &fake.Tracker{}
		common.Tracker = fakeTracker

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()

		common.Tracker = origTracker
	})

	Describe("POST /projects/:name/deployments", func() {
		var (
			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer
			err    error

			u *user.User
			t *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			testhelper.DeleteQueue(mq, queues.All...)

			u, _, t = factories.AuthTrio(db)

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
		})

		doRequestWithMultipart := func(partName, filename string) {
			s = httptest.NewServer(server.New())

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			f, err := os.Open(filename)
			Expect(err).To(BeNil())

			part, err := writer.CreateFormFile(partName, filename)
			Expect(err).To(BeNil())

			_, err = io.Copy(part, f)
			Expect(err).To(BeNil())

			Expect(writer.Close()).To(BeNil())

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

		doRequestWithForm := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/deployments", params, headers, nil)
			Expect(err).To(BeNil())
		}

		doRequestWithBundleChecksum := func(checksum string) {
			doRequestWithForm(url.Values{"bundle_checksum": {checksum}})
		}

		doRequest := func() {
			doRequestWithMultipart("payload", "../../../testhelper/fixtures/website.tar.gz")
		}

		doRequestWithZipFile := func() {
			doRequestWithMultipart("payload", "../../../testhelper/fixtures/website.zip")
		}

		doRequestWithSmallWebsite := func() {
			doRequestWithMultipart("payload", "../../../testhelper/fixtures/small-website.tar.gz")
		}

		doRequestWithWrongPart := func() {
			doRequestWithMultipart("upload", "../../../testhelper/fixtures/website.tar.gz")
		}

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
		}, func() {
			// should not deploy anything if project is not found
			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
			depl := &deployment.Deployment{}
			Expect(db.Last(depl).Error).To(Equal(gorm.RecordNotFound))
		})

		sharedexamples.ItLocksProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, func() {
			// should not deploy anything if project is locked
			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
			depl := &deployment.Deployment{}
			Expect(db.Last(depl).Error).To(Equal(gorm.RecordNotFound))
		})

		Context("when the project belongs to current user", func() {
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
					Expect(db.Last(depl).Error).To(Equal(gorm.RecordNotFound))
				})
			})

			Context("when the payload is larger than the limit", func() {
				var origMaxUploadSize int64

				BeforeEach(func() {
					origMaxUploadSize = s3client.MaxUploadSize
					s3client.MaxUploadSize = 10
					doRequest()
				})

				AfterEach(func() {
					s3client.MaxUploadSize = origMaxUploadSize
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
					Expect(db.Last(depl).Error).To(Equal(gorm.RecordNotFound))
				})
			})

			Context("when the payload is smaller than 512 bytes", func() {
				It("uploads without error", func() {
					doRequestWithSmallWebsite()

					depl := &deployment.Deployment{}
					db.Last(depl)

					Expect(fakeS3.UploadCalls.Count()).To(Equal(1))
					call := fakeS3.UploadCalls.NthCall(1)
					Expect(call).NotTo(BeNil())
					Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
					Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
					Expect(call.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)))
					Expect(call.Arguments[4]).To(Equal(""))
					Expect(call.Arguments[5]).To(Equal("private"))

					b, err := ioutil.ReadFile("../../../testhelper/fixtures/small-website.tar.gz")
					Expect(err).To(BeNil())
					Expect(call.SideEffects["uploaded_content"]).To(Equal(b))
				})
			})

			Context("when the request is valid", func() {
				var depl *deployment.Deployment

				It("returns 202 accepted", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)

					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					j := map[string]interface{}{
						"deployment": map[string]interface{}{
							"id":      depl.ID,
							"state":   deployment.StatePendingBuild,
							"version": 1,
						},
					}
					expectedJSON, err := json.Marshal(j)
					Expect(err).To(BeNil())
					Expect(b.String()).To(MatchJSON(expectedJSON))
				})

				It("uploads zip bundle", func() {
					doRequestWithZipFile()

					depl = &deployment.Deployment{}
					db.Last(depl)

					Expect(fakeS3.UploadCalls.Count()).To(Equal(1))
					call := fakeS3.UploadCalls.NthCall(1)
					Expect(call).NotTo(BeNil())
					Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
					Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
					Expect(call.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.zip", depl.Prefix, depl.ID)))
					Expect(call.Arguments[4]).To(Equal(""))
					Expect(call.Arguments[5]).To(Equal("private"))

					b, err := ioutil.ReadFile("../../../testhelper/fixtures/website.zip")
					Expect(err).To(BeNil())
					Expect(call.SideEffects["uploaded_content"]).To(Equal(b))
				})

				It("creates a deployment record", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)

					bun := &rawbundle.RawBundle{}
					db.Last(bun)

					Expect(depl).NotTo(BeNil())
					Expect(depl.ProjectID).To(Equal(proj.ID))
					Expect(depl.UserID).To(Equal(u.ID))
					Expect(depl.State).To(Equal(deployment.StatePendingBuild))
					Expect(depl.Prefix).NotTo(HaveLen(0))
					Expect(depl.Version).To(Equal(int64(1)))

					Expect(bun).NotTo(BeNil())
					Expect(*depl.RawBundleID).To(Equal(bun.ID))
					Expect(depl.JsEnvVars).To(Equal([]byte("{}")))
				})

				It("creates a bundle record", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)
					bun := &rawbundle.RawBundle{}
					db.Last(bun)

					Expect(bun).NotTo(BeNil())
					Expect(bun.ProjectID).To(Equal(proj.ID))
					Expect(bun.UploadedPath).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)))
					Expect(bun.Checksum).To(Equal("d177de8d751c4bc0cad763ed53523bc10a88d0ef0c8b8814a9170d69ccc76945"))
				})

				It("does not bundle to s3", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)

					Expect(fakeS3.UploadCalls.Count()).To(Equal(1))
					call := fakeS3.UploadCalls.NthCall(1)
					Expect(call).NotTo(BeNil())
					Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
					Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
					Expect(call.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)))
					Expect(call.Arguments[4]).To(Equal(""))
					Expect(call.Arguments[5]).To(Equal("private"))

					b, err := ioutil.ReadFile("../../../testhelper/fixtures/website.tar.gz")
					Expect(err).To(BeNil())
					Expect(call.SideEffects["uploaded_content"]).To(Equal(b))
				})

				It("enqueues a build job", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)

					d := testhelper.ConsumeQueue(mq, queues.Build)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
						{
							"deployment_id": %d,
							"archive_format": "tar.gz"
						}
					`, depl.ID)))
				})

				It("tracks an 'Initiated Project Deployment' event", func() {
					doRequest()

					depl = &deployment.Deployment{}
					db.Last(depl)

					trackCall := fakeTracker.TrackCalls.NthCall(1)
					Expect(trackCall).NotTo(BeNil())
					Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
					Expect(trackCall.Arguments[1]).To(Equal("Initiated Project Deployment"))
					Expect(trackCall.Arguments[2]).To(Equal(""))

					t := trackCall.Arguments[3]
					props, ok := t.(map[string]interface{})
					Expect(ok).To(BeTrue())
					Expect(props["projectName"]).To(Equal(proj.Name))
					Expect(props["deploymentId"]).To(Equal(depl.ID))
					Expect(props["deploymentPrefix"]).To(Equal(depl.Prefix))
					Expect(props["deploymentVersion"]).To(Equal(depl.Version))

					c := trackCall.Arguments[4]
					context, ok := c.(map[string]interface{})
					Expect(ok).To(BeTrue())
					Expect(context["ip"]).ToNot(BeNil())
					Expect(context["user_agent"]).ToNot(BeNil())

					Expect(trackCall.ReturnValues[0]).To(BeNil())
				})

				Describe("when deploying again", func() {
					BeforeEach(func() {
						doRequest()
						depl = &deployment.Deployment{}
						db.Last(depl)
					})

					It("increments version", func() {
						doRequest()

						depl = &deployment.Deployment{}
						db.Last(depl)

						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)

						j := map[string]interface{}{
							"deployment": map[string]interface{}{
								"id":      depl.ID,
								"state":   deployment.StatePendingBuild,
								"version": 2,
							},
						}
						expectedJSON, err := json.Marshal(j)
						Expect(err).To(BeNil())
						Expect(b.String()).To(MatchJSON(expectedJSON))

						Expect(depl).NotTo(BeNil())
						Expect(depl.Version).To(Equal(int64(2)))
					})
				})

				It("enqueues a build job", func() {
					doRequest()
					depl = &deployment.Deployment{}
					db.Last(depl)

					d := testhelper.ConsumeQueue(mq, queues.Build)
					Expect(d).NotTo(BeNil())
					Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
						{
							"deployment_id": %d,
							"archive_format": "tar.gz"
						}
					`, depl.ID)))
				})

				Context("when skip_build is true", func() {
					BeforeEach(func() {
						proj.SkipBuild = true
						Expect(db.Save(proj).Error).To(BeNil())
					})

					It("enqueues a deploy job", func() {
						doRequest()
						depl = &deployment.Deployment{}
						db.Last(depl)

						d := testhelper.ConsumeQueue(mq, queues.Deploy)
						Expect(d).NotTo(BeNil())
						Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
							{
								"deployment_id": %d,
								"skip_webroot_upload": false,
								"skip_invalidation": false,
								"use_raw_bundle": true,
								"archive_format": "tar.gz"
							}
						`, depl.ID)))
					})

					It("update deployment to be `pending_deploy`", func() {
						doRequest()
						depl = &deployment.Deployment{}
						db.Last(depl)

						Expect(depl.State).To(Equal(deployment.StatePendingDeploy))
					})
				})
			})

			Context("when bundle_checksum is specified", func() {
				Context("when raw bundle exists", func() {
					var (
						depl              *deployment.Deployment
						existingRawBundle *rawbundle.RawBundle
						checksum          string
					)

					BeforeEach(func() {
						checksum = "db39e098913eee20e5371139022e4431ffe7b01baa524bd87e08f2763de3ea55"
						existingRawBundle = &rawbundle.RawBundle{
							ProjectID:    proj.ID,
							Checksum:     checksum,
							UploadedPath: "deployments/pr3f1x-1234/raw-bundle.tar.gz",
						}
						Expect(db.Create(existingRawBundle).Error).To(BeNil())
					})

					It("returns 202 accepted", func() {
						doRequestWithBundleChecksum(checksum)
						depl = &deployment.Deployment{}
						db.Last(depl)

						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)

						j := map[string]interface{}{
							"deployment": map[string]interface{}{
								"id":      depl.ID,
								"state":   deployment.StatePendingBuild,
								"version": 1,
							},
						}
						expectedJSON, err := json.Marshal(j)
						Expect(err).To(BeNil())
						Expect(b.String()).To(MatchJSON(expectedJSON))
					})

					It("creates a deployment record", func() {
						doRequestWithBundleChecksum(checksum)
						depl = &deployment.Deployment{}
						db.Last(depl)

						Expect(depl).NotTo(BeNil())
						Expect(depl.ProjectID).To(Equal(proj.ID))
						Expect(depl.UserID).To(Equal(u.ID))
						Expect(depl.State).To(Equal(deployment.StatePendingBuild))
						Expect(depl.Prefix).NotTo(HaveLen(0))
						Expect(depl.Version).To(Equal(int64(1)))

						Expect(existingRawBundle).NotTo(BeNil())
						Expect(*depl.RawBundleID).To(Equal(existingRawBundle.ID))
					})

					It("does not upload bundle to s3", func() {
						doRequestWithBundleChecksum(checksum)
						depl = &deployment.Deployment{}
						db.Last(depl)

						Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
					})

					It("enqueues a build job", func() {
						doRequestWithBundleChecksum(checksum)
						depl = &deployment.Deployment{}
						db.Last(depl)

						m := testhelper.ConsumeQueue(mq, queues.Build)
						Expect(m).NotTo(BeNil())
						Expect(m.Body).To(MatchJSON(fmt.Sprintf(`
							{
								"deployment_id": %d,
								"archive_format": "tar.gz"
							}
						`, depl.ID)))
					})

					Context("when the raw bundle is not associated with the project", func() {
						BeforeEach(func() {
							proj2 := factories.Project(db, u)
							existingRawBundle.ProjectID = proj2.ID
							Expect(db.Save(existingRawBundle).Error).To(BeNil())
						})

						It("returns 422 with invalid request", func() {
							doRequestWithBundleChecksum(checksum)
							depl = &deployment.Deployment{}
							db.Last(depl)

							b := &bytes.Buffer{}
							_, err = b.ReadFrom(res.Body)
							Expect(err).To(BeNil())

							Expect(res.StatusCode).To(Equal(422))
							Expect(b.String()).To(MatchJSON(`{
								"error": "invalid_params",
								"errors": {
									"bundle_checksum": "the bundle could not be found"
								}
							}`))
						})
					})
				})

				Context("when the bundle does not exist", func() {
					BeforeEach(func() {
						doRequestWithBundleChecksum("db39e098913eee20e5371139022e4431ffe7b01baa524bd87e08f2763de3ea55")
					})

					It("returns 422 with invalid request", func() {
						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(422))
						Expect(b.String()).To(MatchJSON(`{
							"error": "invalid_params",
							"errors": {
								"bundle_checksum": "the bundle could not be found"
							}
						}`))
					})
				})

				Context("when the bundle has been deleted", func() {
					var existingRawBundle *rawbundle.RawBundle

					BeforeEach(func() {
						existingRawBundle = &rawbundle.RawBundle{
							ProjectID:    proj.ID,
							Checksum:     "d4e5f6",
							UploadedPath: "deployments/pr3f1x-1234/raw-bundle.tar.gz",
						}
						Expect(db.Create(existingRawBundle).Error).To(BeNil())
						Expect(db.Delete(existingRawBundle).Error).To(BeNil())

						doRequestWithBundleChecksum(existingRawBundle.Checksum)
					})

					It("returns 422 with invalid request", func() {
						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(422))
						Expect(b.String()).To(MatchJSON(`{
							"error": "invalid_params",
							"errors": {
								"bundle_checksum": "the bundle could not be found"
							}
						}`))
					})
				})
			})

			Context("when template_id is specified", func() {
				Context("when template exists", func() {
					var tmpl *template.Template

					BeforeEach(func() {
						tmpl = &template.Template{
							Name:            "Blog",
							Rank:            1,
							DownloadURL:     "/templates/blog.zip",
							PreviewURL:      "",
							PreviewImageURL: "",
						}
						Expect(db.Create(tmpl).Error).To(BeNil())
					})

					It("returns 202 accepted", func() {
						doRequestWithForm(url.Values{
							"template_id": {strconv.Itoa(int(tmpl.ID))},
						})

						Expect(res.StatusCode).To(Equal(http.StatusAccepted))

						depl := &deployment.Deployment{}
						db.Last(depl)

						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)

						j := map[string]interface{}{
							"deployment": map[string]interface{}{
								"id":      depl.ID,
								"state":   deployment.StatePendingBuild,
								"version": 1,
							},
						}
						expectedJSON, err := json.Marshal(j)
						Expect(err).To(BeNil())
						Expect(b.String()).To(MatchJSON(expectedJSON))
					})

					It("creates a deployment record and a raw_bundle record", func() {
						doRequestWithForm(url.Values{
							"template_id": {strconv.Itoa(int(tmpl.ID))},
						})

						depl := &deployment.Deployment{}
						db.Last(depl)

						Expect(depl).NotTo(BeNil())
						Expect(depl.ProjectID).To(Equal(proj.ID))
						Expect(depl.UserID).To(Equal(u.ID))
						Expect(depl.State).To(Equal(deployment.StatePendingBuild))
						Expect(depl.Prefix).NotTo(HaveLen(0))
						Expect(depl.Version).To(Equal(int64(1)))
						Expect(depl.RawBundleID).NotTo(BeNil())

						Expect(*depl.TemplateID).To(Equal(tmpl.ID))

						bun := &rawbundle.RawBundle{}
						db.First(bun, *depl.RawBundleID)

						Expect(bun).NotTo(BeNil())
						Expect(bun.UploadedPath).To(Equal("deployments/" + depl.PrefixID() + "/raw-bundle.zip"))
					})

					It("does not upload bundle to S3", func() {
						doRequestWithForm(url.Values{
							"template_id": {strconv.Itoa(int(tmpl.ID))},
						})

						Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
					})

					It("makes a copy of the template on S3", func() {
						doRequestWithForm(url.Values{
							"template_id": {strconv.Itoa(int(tmpl.ID))},
						})

						Expect(fakeS3.CopyCalls.Count()).To(Equal(1))
						call := fakeS3.CopyCalls.NthCall(1)
						Expect(call).NotTo(BeNil())
						Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
						Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
						Expect(call.Arguments[2]).To(Equal(tmpl.DownloadURL))

						depl := &deployment.Deployment{}
						db.Last(depl)

						Expect(call.Arguments[3]).To(Equal("deployments/" + depl.PrefixID() + "/raw-bundle.zip"))
					})

					It("enqueues a build job", func() {
						doRequestWithForm(url.Values{
							"template_id": {strconv.Itoa(int(tmpl.ID))},
						})

						depl := &deployment.Deployment{}
						db.Last(depl)

						m := testhelper.ConsumeQueue(mq, queues.Build)
						Expect(m).NotTo(BeNil())
						Expect(m.Body).To(MatchJSON(fmt.Sprintf(`
							{
								"deployment_id": %d,
								"archive_format": "zip"
							}
						`, depl.ID)))
					})
				})

				Context("when template_id is not a valid integer", func() {
					It("returns 422", func() {
						doRequestWithForm(url.Values{
							"template_id": {"not-an-integer"},
						})

						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						_, err = b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(422))
						Expect(b.String()).To(MatchJSON(`{
							"error": "invalid_params",
							"errors": {
								"template_id": "is invalid"
							}
						}`))
					})
				})

				Context("when template does not exist", func() {
					It("returns 422", func() {
						doRequestWithForm(url.Values{
							"template_id": {"999"},
						})

						b := &bytes.Buffer{}
						_, err = b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(422))
						Expect(b.String()).To(MatchJSON(`{
							"error": "invalid_params",
							"errors": {
								"template_id": "is not that of a known template"
							}
						}`))
					})
				})
			})

			Context("when the request is valid and previous active deployment exists", func() {
				var depl *deployment.Deployment

				BeforeEach(func() {
					depl := factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
						JsEnvVars: []byte(`{"foo":"bar","express":"com"}`),
						State:     deployment.StateDeployed,
					})
					proj.ActiveDeploymentID = &depl.ID
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("copies the js env vars from previous active deployment", func() {
					doRequest()
					depl = &deployment.Deployment{}
					db.Last(depl)

					Expect(depl).NotTo(BeNil())
					Expect(depl.ProjectID).To(Equal(proj.ID))
					Expect(depl.UserID).To(Equal(u.ID))
					Expect(depl.State).To(Equal(deployment.StatePendingBuild))
					Expect(depl.Prefix).NotTo(HaveLen(0))
					Expect(depl.Version).To(Equal(int64(2)))
					Expect(depl.JsEnvVars).To(Equal([]byte(`{"foo":"bar","express":"com"}`)))
				})
			})
		})
	})

	Describe("GET /projects/:project_name/deployments/:id", func() {
		var (
			err error

			u *user.User
			t *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			depl    *deployment.Deployment
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

			errorMessage := "index.js:Missing Parent"
			depl = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:       "a1b2c3",
				State:        deployment.StatePendingDeploy,
				DeployedAt:   timeAgo(-1 * time.Hour),
				ErrorMessage: &errorMessage,
			})
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			url := fmt.Sprintf("%s/projects/foo-bar-express/deployments/%d", s.URL, depl.ID)
			res, err = testhelper.MakeRequest("GET", url, nil, headers, nil)
			Expect(err).To(BeNil())
		}

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

		Context("the deployment exist", func() {
			It("returns 200 status ok", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusOK))

				var d deployment.Deployment
				Expect(db.First(&d, depl.ID).Error).To(BeNil())
				j := map[string]interface{}{
					"deployment": map[string]interface{}{
						"id":            d.ID,
						"state":         deployment.StatePendingDeploy,
						"deployed_at":   d.DeployedAt,
						"version":       d.Version,
						"error_message": d.ErrorMessage,
					},
				}
				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))
			})
		})

		Context("the deployment does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(depl).Error).To(BeNil())
			})

			It("returns 404 not found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "deployment could not be found"
				}`))
			})
		})

		Context("the deployment id is not a number", func() {
			BeforeEach(func() {
				s = httptest.NewServer(server.New())
				url := fmt.Sprintf("%s/projects/foo-bar-express/deployments/cafebabe", s.URL)
				res, err = testhelper.MakeRequest("GET", url, nil, headers, nil)
				Expect(err).To(BeNil())
			})

			It("returns 404 not found", func() {
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "deployment could not be found"
				}`))
			})
		})
	})

	Describe("GET /projects/:project_name/deployments/:id/download", func() {
		var (
			err error

			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			u *user.User
			t *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			depl    *deployment.Deployment
			bun     *rawbundle.RawBundle
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			u, _, t = factories.AuthTrio(db)

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			bun = factories.RawBundle(db, proj)

			depl = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:      "a1b2c3",
				State:       deployment.StateDeployed,
				DeployedAt:  timeAgo(-1 * time.Hour),
				RawBundleID: &bun.ID,
			})
		})

		AfterEach(func() {
			s3client.S3 = origS3
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			url := fmt.Sprintf("%s/projects/foo-bar-express/deployments/%d/download", s.URL, depl.ID)
			res, err = testhelper.MakeRequest("GET", url, nil, headers, nil)
			Expect(err).To(BeNil())
		}

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

		Context("when the deployment does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(depl).Error).To(BeNil())
			})

			It("responds with 404 Not Found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "deployment could not be found"
				}`))
			})
		})

		Context("when the deployment id is not a number", func() {
			BeforeEach(func() {
				s = httptest.NewServer(server.New())
				url := fmt.Sprintf("%s/projects/foo-bar-express/deployments/cafebabe", s.URL)
				res, err = testhelper.MakeRequest("GET", url, nil, headers, nil)
				Expect(err).To(BeNil())
			})

			It("responds with 404 Not Found", func() {
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "deployment could not be found"
				}`))
			})
		})

		Context("when the deployment does not have an associated RawBundle", func() {
			BeforeEach(func() {
				depl.RawBundleID = nil
				Expect(db.Save(depl).Error).To(BeNil())
			})

			It("responds with 404 Not Found", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "deployment cannot be downloaded"
				}`))
			})
		})

		Context("when the deployment's associated RawBundle does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(bun).Error).To(BeNil())
			})

			It("responds with 410 Gone", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusGone))
				Expect(b.String()).To(MatchJSON(`{
					"error": "gone",
					"error_description": "deployment can no longer be downloaded"
				}`))
			})
		})

		Context("when raw bundle does not exist on S3", func() {
			BeforeEach(func() {
				fakeS3.ExistsReturn = false
			})

			It("responds with 410 Gone", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusGone))
				Expect(b.String()).To(MatchJSON(`{
					"error": "gone",
					"error_description": "deployment can no longer be downloaded"
				}`))

				Expect(fakeS3.ExistsCalls.Count()).To(Equal(1))
			})
		})

		Context("when raw bundle exists on S3", func() {
			BeforeEach(func() {
				fakeS3.ExistsReturn = true
				fakeS3.PresignedURLReturn = "https://s3-us-west-2.amazonaws.com/deployments/abcd/raw-bundle.zip?abc=123"
			})

			It("responds with a pre-signed download URL of the raw bundle in S3", func() {
				doRequest()

				Expect(fakeS3.PresignedURLCalls.Count()).To(Equal(1))
				call := fakeS3.PresignedURLCalls.NthCall(1)
				Expect(call).NotTo(BeNil())
				Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(call.Arguments[2]).To(Equal(bun.UploadedPath))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"url": "%s"
				}`, fakeS3.PresignedURLReturn)))
			})
		})
	})

	Describe("POST /projects/:project_name/rollback", func() {
		var (
			err error

			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			mq *amqp.Connection

			u *user.User
			t *oauthtoken.OauthToken

			params  url.Values
			headers http.Header
			proj    *project.Project

			dm1 *domain.Domain
			dm2 *domain.Domain

			depl1 *deployment.Deployment
			depl2 *deployment.Deployment
			depl3 *deployment.Deployment
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			testhelper.DeleteQueue(mq, queues.All...)

			u, _, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			dm1 = factories.Domain(db, proj)
			dm2 = factories.Domain(db, proj)

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			depl1 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:     "a1b2c3",
				State:      deployment.StateDeployed,
				DeployedAt: timeAgo(3 * time.Hour),
			})

			depl2 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix: "a7b8c9",
				State:  deployment.StateUploaded,
			})

			depl3 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:     "d1e2f3",
				State:      deployment.StateDeployed,
				DeployedAt: timeAgo(1 * time.Hour),
			})

			var currentDeplID = depl3.ID
			proj.ActiveDeploymentID = &currentDeplID
			Expect(db.Save(proj).Error).To(BeNil())
		})

		AfterEach(func() {
			s3client.S3 = origS3
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			url := fmt.Sprintf("%s/projects/foo-bar-express/rollback", s.URL)
			res, err = testhelper.MakeRequest("POST", url, params, headers, nil)
			Expect(err).To(BeNil())
		}

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

		Context("when the version is not specified", func() {
			It("returns 202 accepted", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				var d deployment.Deployment
				Expect(db.First(&d, depl1.ID).Error).To(BeNil())
				j := map[string]interface{}{
					"deployment": map[string]interface{}{
						"id":          d.ID,
						"state":       deployment.StatePendingRollback,
						"deployed_at": d.DeployedAt,
						"version":     d.Version,
					},
				}
				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))
			})

			It("enqueues a build job", func() {
				doRequest()

				d := testhelper.ConsumeQueue(mq, queues.Deploy)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": false,
						"use_raw_bundle": false
					}
				`, depl1.ID)))
			})

			It("marks the deployment as 'pending_rollback'", func() {
				doRequest()

				var updatedDeployment deployment.Deployment
				Expect(db.First(&updatedDeployment, depl1.ID).Error).To(BeNil())
				Expect(updatedDeployment.State).To(Equal(deployment.StatePendingRollback))
			})

			It("tracks an 'Initiated Project Rollback' event", func() {
				doRequest()

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Initiated Project Rollback"))
				Expect(trackCall.Arguments[2]).To(Equal(""))

				t := trackCall.Arguments[3]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal(proj.Name))
				Expect(props["deployedVersion"]).To(Equal(depl3.Version))
				Expect(props["targetVersion"]).To(Equal(depl1.Version))

				c := trackCall.Arguments[4]
				context, ok := c.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(context["ip"]).ToNot(BeNil())
				Expect(context["user_agent"]).ToNot(BeNil())

				Expect(trackCall.ReturnValues[0]).To(BeNil())
			})
		})

		Context("when the version is specified", func() {
			var depl4 *deployment.Deployment

			BeforeEach(func() {
				depl4 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
					Prefix:     "x0y1z2",
					State:      deployment.StateDeployed,
					DeployedAt: timeAgo(2 * time.Hour),
				})

				params = url.Values{
					"version": {strconv.FormatInt(depl4.Version, 10)},
				}
			})

			AfterEach(func() {
				params = url.Values{}
			})

			It("returns 202 accepted", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				var d deployment.Deployment
				Expect(db.First(&d, depl4.ID).Error).To(BeNil())
				j := map[string]interface{}{
					"deployment": map[string]interface{}{
						"id":          d.ID,
						"state":       deployment.StatePendingRollback,
						"deployed_at": d.DeployedAt,
						"version":     d.Version,
					},
				}
				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))
			})

			It("enqueues a deploy job", func() {
				doRequest()

				d := testhelper.ConsumeQueue(mq, queues.Deploy)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": false,
						"use_raw_bundle": false
					}
				`, depl4.ID)))
			})

			It("marks the deployment as 'pending_rollback'", func() {
				doRequest()

				var updatedDeployment deployment.Deployment
				Expect(db.First(&updatedDeployment, depl4.ID).Error).To(BeNil())
				Expect(updatedDeployment.State).To(Equal(deployment.StatePendingRollback))
			})

			It("tracks an 'Initiated Project Rollback' event", func() {
				doRequest()

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Initiated Project Rollback"))
				Expect(trackCall.Arguments[2]).To(Equal(""))

				t := trackCall.Arguments[3]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal(proj.Name))
				Expect(props["deployedVersion"]).To(Equal(depl3.Version))
				Expect(props["targetVersion"]).To(Equal(depl4.Version))

				c := trackCall.Arguments[4]
				context, ok := c.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(context["ip"]).ToNot(BeNil())
				Expect(context["user_agent"]).ToNot(BeNil())

				Expect(trackCall.ReturnValues[0]).To(BeNil())
			})

			Context("when the deployment does not exist", func() {
				BeforeEach(func() {
					Expect(db.Delete(depl4).Error).To(BeNil())
				})

				It("returns 422 with invalid_request", func() {
					doRequest()
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "completed deployment with a given version could not be found"
					}`))
				})

				It("does not track any 'Initiated Project Rollback' event", func() {
					doRequest()

					trackCall := fakeTracker.TrackCalls.NthCall(1)
					Expect(trackCall).To(BeNil())
				})
			})

			Context("when the deployment is already active", func() {
				BeforeEach(func() {
					proj.ActiveDeploymentID = &depl4.ID
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("returns 422 with invalid_request", func() {
					doRequest()
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "the specified deployment is already active"
					}`))
				})

				It("does not track any 'Initiated Project Rollback' event", func() {
					doRequest()

					trackCall := fakeTracker.TrackCalls.NthCall(1)
					Expect(trackCall).To(BeNil())
				})
			})

			Context("when the deployment is not in deployed state", func() {
				BeforeEach(func() {
					depl4.State = deployment.StatePendingUpload
					Expect(db.Save(depl4).Error).To(BeNil())
				})

				It("returns 422 with invalid_request", func() {
					doRequest()
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "completed deployment with a given version could not be found"
					}`))
				})
			})

			Context("when the deployment does not belong to the project", func() {
				BeforeEach(func() {
					proj2 := factories.Project(db, u)
					depl4.ProjectID = proj2.ID
					Expect(db.Save(depl4).Error).To(BeNil())
				})

				It("returns 422 with invalid_request", func() {
					doRequest()
					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "completed deployment with a given version could not be found"
					}`))
				})
			})
		})

		Context("when active_deployment_id is nil", func() {
			BeforeEach(func() {
				proj.ActiveDeploymentID = nil
				Expect(db.Save(proj).Error).To(BeNil())
			})

			It("returns 412 with precondition_failed", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusPreconditionFailed))
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(`{
					"error": "precondition_failed",
					"error_description": "active deployment could not be found"
				}`))
			})
		})

		Context("when there is no previous completed deployment", func() {
			BeforeEach(func() {
				proj.ActiveDeploymentID = &depl1.ID
				Expect(db.Save(proj).Error).To(BeNil())
			})

			It("returns 412 with precondition_failed", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusPreconditionFailed))
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(`{
					"error": "precondition_failed",
					"error_description": "previous completed deployment could not be found"
				}`))
			})
		})
	})

	Describe("GET /projects/:name/deployments", func() {
		var (
			err error

			u *user.User
			t *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			depl1   *deployment.Deployment
			depl2   *deployment.Deployment
			depl3   *deployment.Deployment
			depl4   *deployment.Deployment
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

			depl1 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:     "a1b2c3",
				State:      deployment.StateDeployed,
				DeployedAt: timeAgo(3 * time.Hour),
			})

			depl2 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:     "d0e1f2",
				State:      deployment.StateDeployed,
				DeployedAt: timeAgo(2 * time.Hour),
			})

			depl3 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix: "x0y1z2",
				State:  deployment.StatePendingDeploy,
			})

			depl4 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				Prefix:     "u0v1w2",
				State:      deployment.StateDeployed,
				DeployedAt: timeAgo(4 * time.Hour),
			})

			proj.ActiveDeploymentID = &depl2.ID
			Expect(db.Save(proj).Error).To(BeNil())
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			url := fmt.Sprintf("%s/projects/foo-bar-express/deployments", s.URL)
			res, err = testhelper.MakeRequest("GET", url, nil, headers, nil)
			Expect(err).To(BeNil())
		}

		formattedTimeForJSON := func(t *time.Time) string {
			formattedTime, err := t.MarshalJSON()
			Expect(err).To(BeNil())
			return string(formattedTime)
		}

		reloadDeployment := func(d *deployment.Deployment) *deployment.Deployment {
			var reloadedDepl deployment.Deployment
			Expect(db.First(&reloadedDepl, d.ID).Error).To(BeNil())
			return &reloadedDepl
		}

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

		It("returns all completed deployments", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusOK))

			depl1 = reloadDeployment(depl1)
			depl2 = reloadDeployment(depl2)
			depl4 = reloadDeployment(depl4)

			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"deployments": [
					{
						"id": %d,
						"state": "%s",
						"active": true,
						"deployed_at": %s,
						"version": %d
					},
					{
						"id": %d,
						"state": "%s",
						"deployed_at": %s,
						"version": %d
					},
					{
						"id": %d,
						"state": "%s",
						"deployed_at": %s,
						"version": %d
					}
				]
			}`, depl2.ID, depl2.State, formattedTimeForJSON(depl2.DeployedAt), depl2.Version,
				depl1.ID, depl1.State, formattedTimeForJSON(depl1.DeployedAt), depl1.Version,
				depl4.ID, depl4.State, formattedTimeForJSON(depl4.DeployedAt), depl4.Version,
			)))
		})

		Context("when project has a limit on max deployments kept", func() {
			BeforeEach(func() {
				proj.MaxDeploysKept = 1
				Expect(db.Save(proj).Error).To(BeNil())
			})

			It("returns only those deployments", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())
				Expect(res.StatusCode).To(Equal(http.StatusOK))

				depl2 = reloadDeployment(depl2)

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"deployments": [
						{
							"id": %d,
							"state": "%s",
							"active": true,
							"deployed_at": %s,
							"version": %d
						}
					]
				}`, depl2.ID, depl2.State, formattedTimeForJSON(depl2.DeployedAt), depl2.Version,
				)))
			})
		})
	})
})
