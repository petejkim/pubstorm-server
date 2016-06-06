package deployer_test

import (
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/deployer/deployer"
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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "deployer")
}

var _ = Describe("Deployer", func() {
	var (
		fakeS3 *fake.S3
		origS3 filetransfer.FileTransfer
		err    error

		fakeTracker *fake.Tracker
		origTracker tracker.Trackable

		db    *gorm.DB
		mq    *amqp.Connection
		qName string

		u    *user.User
		proj *project.Project
		depl *deployment.Deployment
	)

	BeforeEach(func() {
		origS3 = s3client.S3
		fakeS3 = &fake.S3{}
		deployer.S3 = fakeS3

		origTracker = common.Tracker
		fakeTracker = &fake.Tracker{}
		common.Tracker = fakeTracker

		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)
		testhelper.DeleteExchange(mq, exchanges.All...)
		qName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)

		u = factories.User(db)
		proj = factories.Project(db, u)
		factories.Domain(db, proj, "www.foo-bar-express.com")

		depl = factories.Deployment(db, proj, u, deployment.StatePendingDeploy)
	})

	AfterEach(func() {
		deployer.S3 = origS3
		common.Tracker = origTracker
	})

	assertUpload := func(nthUpload int, uploadPath, contentType string, content []byte) {
		uploadCall := fakeS3.UploadCalls.NthCall(nthUpload)
		Expect(uploadCall).NotTo(BeNil())
		Expect(uploadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
		Expect(uploadCall.Arguments[1]).To(Equal(s3client.BucketName))
		Expect(uploadCall.Arguments[2]).To(Equal(uploadPath))
		Expect(uploadCall.Arguments[4]).To(Equal(contentType))
		Expect(uploadCall.Arguments[5]).To(Equal("public-read"))
		Expect(uploadCall.ReturnValues[0]).To(BeNil())
		if contentType == "application/json" {
			Expect(uploadCall.SideEffects["uploaded_content"]).To(MatchJSON(content))
		} else {
			Expect(uploadCall.SideEffects["uploaded_content"]).To(Equal(content))
		}
	}

	assertActiveDeploymentIDUpdate := func() {
		err = db.First(proj, proj.ID).Error
		Expect(err).To(BeNil())

		Expect(*proj.ActiveDeploymentID).To(Equal(depl.ID))
	}

	assertMetaDataUpload := func() {
		Expect(fakeS3.UploadCalls.Count()).To(Equal(2)) // 2 metadata files (2 domains)

		for i, domain := range []string{
			proj.DefaultDomainName(),
			"www.foo-bar-express.com",
		} {
			assertUpload(
				1+i,
				"domains/"+domain+"/meta.json",
				"application/json",
				[]byte(fmt.Sprintf(`{
					"prefix": "%s"
				}`, depl.PrefixID())),
			)
		}
	}

	It("fetches the optimized bundle from S3, uploads assets and meta data to S3, and publishes invalidation message to edges", func() {
		// mock download
		fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
		Expect(err).To(BeNil())

		err = deployer.Work([]byte(fmt.Sprintf(`{
			"deployment_id": %d
		}`, depl.ID)))
		Expect(err).To(BeNil())

		// it should download raw bundle from s3
		Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
		downloadCall := fakeS3.DownloadCalls.NthCall(1)
		Expect(downloadCall).NotTo(BeNil())
		Expect(downloadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
		Expect(downloadCall.Arguments[1]).To(Equal(s3client.BucketName))
		Expect(downloadCall.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s/optimized-bundle.tar.gz", depl.PrefixID())))
		Expect(downloadCall.ReturnValues[0]).To(BeNil())

		// it should upload assets
		Expect(fakeS3.UploadCalls.Count()).To(Equal(7)) // 5 asset files + 2 metadata files (2 domains)

		uploads := []struct {
			filename    string
			contentType string
		}{
			{"images/rick-astley.jpg", "image/jpeg"},
			{"images/astley.jpg", "image/jpeg"},
			{"index.html", "text/html"},
			{"js/app.js", "application/javascript"},
			{"css/app.css", "text/css"},
		}

		for i, upload := range uploads {
			data, err := ioutil.ReadFile("../../testhelper/fixtures/website/" + upload.filename)
			Expect(err).To(BeNil())

			assertUpload(
				1+i,
				"deployments/"+depl.PrefixID()+"/webroot/"+upload.filename,
				upload.contentType,
				data,
			)
		}

		// it should upload meta.json for each domain
		for i, domain := range []string{
			proj.DefaultDomainName(),
			"www.foo-bar-express.com",
		} {
			assertUpload(
				6+i,
				"domains/"+domain+"/meta.json",
				"application/json",
				[]byte(fmt.Sprintf(`{
					"prefix": "%s"
				}`, depl.PrefixID())),
			)
		}

		// it should publish invalidation message
		d := testhelper.ConsumeQueue(mq, qName)
		Expect(d).NotTo(BeNil())
		Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
			"domains": [
				"%s",
				"www.foo-bar-express.com"
			]
		}`, proj.DefaultDomainName())))

		// it should update deployment's state to deployed
		err = db.First(depl, depl.ID).Error
		Expect(err).To(BeNil())

		Expect(depl.State).To(Equal(deployment.StateDeployed))
		Expect(depl.DeployedAt).NotTo(BeNil())
		Expect(depl.DeployedAt.Unix()).To(BeNumerically("~", time.Now().Unix(), 1))

		// it should set project's active deployment to current deployment id
		assertActiveDeploymentIDUpdate()
	})

	It("tracks a 'Project Deployed' event", func() {
		fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
		Expect(err).To(BeNil())

		err = deployer.Work([]byte(fmt.Sprintf(`{
			"deployment_id": %d
		}`, depl.ID)))
		Expect(err).To(BeNil())

		trackCall := fakeTracker.TrackCalls.NthCall(1)
		Expect(trackCall).NotTo(BeNil())
		Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
		Expect(trackCall.Arguments[1]).To(Equal("Project Deployed"))

		t := trackCall.Arguments[2]
		props, ok := t.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(props["projectName"]).To(Equal(proj.Name))
		Expect(props["deploymentId"]).To(Equal(depl.ID))
		Expect(props["deploymentPrefix"]).To(Equal(depl.Prefix))
		Expect(props["deploymentVersion"]).To(Equal(depl.Version))

		err = db.First(depl, depl.ID).Error
		Expect(err).To(BeNil())

		startTime := depl.CreatedAt
		Expect(depl.DeployedAt).NotTo(BeNil())
		endTime := *depl.DeployedAt
		dur := endTime.Sub(startTime)
		Expect(props["timeTakenInSeconds"]).To(Equal(int64(dur / time.Second)))

		Expect(trackCall.Arguments[3]).To(BeNil())
		Expect(trackCall.ReturnValues[0]).To(BeNil())
	})

	Context("when default domain is disabled", func() {
		BeforeEach(func() {
			proj.DefaultDomainEnabled = false
			Expect(db.Save(proj).Error).To(BeNil())
		})

		It("does not deploy to the default domain", func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
			Expect(err).To(BeNil())

			err = deployer.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl.ID)))
			Expect(err).To(BeNil())

			// it should upload meta.json for each domain
			for i, domain := range []string{
				"www.foo-bar-express.com",
			} {
				assertUpload(
					6+i,
					"domains/"+domain+"/meta.json",
					"application/json",
					[]byte(fmt.Sprintf(`{
						"prefix": "%s"
					}`, depl.PrefixID())),
				)
			}

			// it should publish invalidation message
			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{
				"domains": [
					"www.foo-bar-express.com"
				]
			}`))
		})
	})

	Context("when project has ForceHTTPS enabled", func() {
		BeforeEach(func() {
			proj.ForceHTTPS = true
			Expect(db.Save(proj).Error).To(BeNil())
		})

		It("uploads meta.json with force_https set", func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
			Expect(err).To(BeNil())

			err = deployer.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl.ID)))
			Expect(err).To(BeNil())

			for i, domain := range []string{
				proj.DefaultDomainName(),
				"www.foo-bar-express.com",
			} {
				assertUpload(
					6+i,
					"domains/"+domain+"/meta.json",
					"application/json",
					[]byte(fmt.Sprintf(`{
						"prefix": "%s",
						"force_https": true
					}`, depl.PrefixID())),
				)
			}
		})
	})

	Context("when skip_webroot_upload is true", func() {
		assertMetaDataUpload := func(doms []string) {
			Expect(fakeS3.UploadCalls.Count()).To(Equal(len(doms)))

			for i, domain := range doms {
				assertUpload(
					1+i,
					"domains/"+domain+"/meta.json",
					"application/json",
					[]byte(fmt.Sprintf(`{
						"prefix": "%s"
					}`, depl.PrefixID())),
				)
			}
		}

		It("only uploads metadata to S3, and publishes invalidation message to edges", func() {
			err = deployer.Work([]byte(fmt.Sprintf(`
				{
					"deployment_id": %d,
					"skip_webroot_upload": true
				}
			`, depl.ID)))
			Expect(err).To(BeNil())

			// it should upload meta.json for each domain
			assertMetaDataUpload([]string{
				proj.Name + "." + shared.DefaultDomain,
				"www.foo-bar-express.com",
			})

			// it should publish invalidation message
			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"domains": [
					"%s",
					"www.foo-bar-express.com"
				]
			}`, proj.DefaultDomainName())))

			// it should set project's active deployment to current deployment id
			assertActiveDeploymentIDUpdate()
		})

		Context("when default domain is disabled", func() {
			BeforeEach(func() {
				proj.DefaultDomainEnabled = false
				Expect(db.Save(proj).Error).To(BeNil())
			})

			It("does not publish invalidation message for the default domain", func() {
				err = deployer.Work([]byte(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": true
					}
				`, depl.ID)))
				Expect(err).To(BeNil())

				assertMetaDataUpload([]string{
					"www.foo-bar-express.com",
				})

				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(`{
					"domains": [
						"www.foo-bar-express.com"
					]
				}`))
			})
		})

		Context("when skip_invalidation is also true", func() {
			It("only uploads metadata to s3, and does not publish invalidation message", func() {
				err = deployer.Work([]byte(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": true,
						"skip_invalidation": true
					}
				`, depl.ID)))
				Expect(err).To(BeNil())

				// it should upload meta.json for each domain
				assertMetaDataUpload([]string{
					proj.Name + "." + shared.DefaultDomain,
					"www.foo-bar-express.com",
				})

				// it should NOT publish invalidation message
				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).To(BeNil())

				// it should set project's active deployment to current deployment id
				assertActiveDeploymentIDUpdate()
			})

			Context("when default domain is disabled", func() {
				BeforeEach(func() {
					proj.DefaultDomainEnabled = false
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("does not upload meta.json for the default domain", func() {
					err = deployer.Work([]byte(fmt.Sprintf(`
						{
							"deployment_id": %d,
							"skip_webroot_upload": true,
							"skip_invalidation": true
						}
					`, depl.ID)))
					Expect(err).To(BeNil())

					// it should upload meta.json
					assertMetaDataUpload([]string{
						"www.foo-bar-express.com",
					})
				})
			})
		})
	})

	Context("the deployment is not in expected state", func() {
		BeforeEach(func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
			Expect(err).To(BeNil())
		})

		It("returns an error if the deployment is in `uploaded` state", func() {
			depl.State = deployment.StateUploaded
			Expect(db.Save(depl).Error).To(BeNil())

			err = deployer.Work([]byte(fmt.Sprintf(`{ "deployment_id": %d }`, depl.ID)))
			Expect(err).NotTo(BeNil())

			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).To(BeNil())

			err = db.First(proj, proj.ID).Error
			Expect(err).To(BeNil())

			Expect(proj.ActiveDeploymentID).To(BeNil())
		})

		It("returns an error if the deployment is in `pending_uploaded` state", func() {
			depl.State = deployment.StatePendingUpload
			Expect(db.Save(depl).Error).To(BeNil())

			err = deployer.Work([]byte(fmt.Sprintf(`{ "deployment_id": %d }`, depl.ID)))
			Expect(err).NotTo(BeNil())

			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).To(BeNil())

			err = db.First(proj, proj.ID).Error
			Expect(err).To(BeNil())

			Expect(proj.ActiveDeploymentID).To(BeNil())
		})

		// We should not allow to upload again for same deployment
		Context("the deployment is in `deployed` state", func() {
			BeforeEach(func() {
				depl.State = deployment.StateDeployed
				Expect(db.Save(depl).Error).To(BeNil())
			})

			It("returns an error if skip_webroot_upload is false", func() {
				err = deployer.Work([]byte(fmt.Sprintf(`{ "deployment_id": %d }`, depl.ID)))
				Expect(err).NotTo(BeNil())

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).To(BeNil())

				err = db.First(proj, proj.ID).Error
				Expect(err).To(BeNil())

				Expect(proj.ActiveDeploymentID).To(BeNil())
			})

			It("only uploads metadata to s3, and does not publish invalidation message if skip_webroot_upload is true", func() {
				err = deployer.Work([]byte(fmt.Sprintf(`
					{
						"deployment_id": %d,
						"skip_webroot_upload": true
					}
				`, depl.ID)))
				Expect(err).To(BeNil())

				// it should upload meta.json for each domain
				assertMetaDataUpload()

				// it should publish invalidation message
				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"domains": [
					"%s",
					"www.foo-bar-express.com"
				]
			}`, proj.DefaultDomainName())))

				// it should set project's active deployment to current deployment id
				assertActiveDeploymentIDUpdate()
			})
		})
	})

	Context("when `use_raw_bundle` is true", func() {
		It("downloads raw bundle to deploy", func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
			Expect(err).To(BeNil())

			err = deployer.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d,
				"use_raw_bundle": true
			}`, depl.ID)))
			Expect(err).To(BeNil())

			// it should download raw bundle from s3
			Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
			downloadCall := fakeS3.DownloadCalls.NthCall(1)
			Expect(downloadCall).NotTo(BeNil())
			Expect(downloadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(downloadCall.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(downloadCall.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s/raw-bundle.tar.gz", depl.PrefixID())))
			Expect(downloadCall.ReturnValues[0]).To(BeNil())
		})
	})
})
