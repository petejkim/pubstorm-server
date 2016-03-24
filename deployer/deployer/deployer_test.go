package deployer_test

import (
	"io/ioutil"
	"testing"

	"github.com/nitrous-io/rise-server/deployer/deployer"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/s3"
	"github.com/nitrous-io/rise-server/testhelper"
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
		fakeS3 *fake.FileTransfer
		origS3 filetransfer.FileTransfer
		err    error

		mq    *amqp.Connection
		qName string
	)

	BeforeEach(func() {
		origS3 = s3.S3
		fakeS3 = &fake.FileTransfer{}
		deployer.S3 = fakeS3

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.DeleteExchange(mq, exchanges.All...)
		qName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)
	})

	AfterEach(func() {
		deployer.S3 = origS3
	})

	It("fetches the raw bundle from S3, uploads assets and meta data to S3, and publishes invalidation message to edges", func() {
		// mock download
		fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
		Expect(err).To(BeNil())

		err = deployer.Work([]byte(`
			{
				"deployment_id": 123,
				"deployment_prefix": "a1b2c3",
				"project_name": "foo-bar-express",
				"domains": [
					"foo-bar-express.rise.cloud",
					"www.foo-bar-express.com"
				]
			}
		`))
		Expect(err).To(BeNil())

		Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
		downloadCall := fakeS3.DownloadCalls.NthCall(1)
		Expect(downloadCall).NotTo(BeNil())
		Expect(downloadCall.Arguments[0]).To(Equal(s3.BucketRegion))
		Expect(downloadCall.Arguments[1]).To(Equal(s3.BucketName))
		Expect(downloadCall.Arguments[2]).To(Equal("deployments/a1b2c3-123/raw-bundle.tar.gz"))
		Expect(downloadCall.ReturnValues[0]).To(BeNil())

		Expect(fakeS3.UploadCalls.Count()).To(Equal(6)) // 4 asset files + 2 metadata files (2 domains)

		uploads := []struct {
			filename    string
			contentType string
		}{
			{"images/rick-astley.jpg", "image/jpeg"},
			{"index.html", "text/html"},
			{"js/app.js", "application/javascript"},
			{"css/app.css", "text/css"},
		}

		for i, upload := range uploads {
			uploadCall := fakeS3.UploadCalls.NthCall(i + 1)
			Expect(uploadCall).NotTo(BeNil())
			Expect(uploadCall.Arguments[0]).To(Equal(s3.BucketRegion))
			Expect(uploadCall.Arguments[1]).To(Equal(s3.BucketName))
			Expect(uploadCall.Arguments[2]).To(Equal("deployments/a1b2c3-123/webroot/" + upload.filename))
			Expect(uploadCall.Arguments[4]).To(Equal(upload.contentType))
			Expect(uploadCall.Arguments[5]).To(Equal("public-read"))
			Expect(uploadCall.ReturnValues[0]).To(BeNil())

			data, err := ioutil.ReadFile("../../testhelper/fixtures/website/" + upload.filename)
			Expect(err).To(BeNil())
			Expect(uploadCall.SideEffects["uploaded_content"]).To(Equal(data))
		}

		for i, domain := range []string{"foo-bar-express.rise.cloud", "www.foo-bar-express.com"} {
			uploadCall := fakeS3.UploadCalls.NthCall(5 + i)
			Expect(uploadCall).NotTo(BeNil())
			Expect(uploadCall.Arguments[0]).To(Equal(s3.BucketRegion))
			Expect(uploadCall.Arguments[1]).To(Equal(s3.BucketName))
			Expect(uploadCall.Arguments[2]).To(Equal("domains/" + domain + "/meta.json"))
			Expect(uploadCall.Arguments[4]).To(Equal("application/json"))
			Expect(uploadCall.Arguments[5]).To(Equal("public-read"))
			Expect(uploadCall.ReturnValues[0]).To(BeNil())
			Expect(uploadCall.SideEffects["uploaded_content"]).To(MatchJSON(`{
				"prefix": "a1b2c3-123"
			}`))
		}

		d := testhelper.ConsumeQueue(mq, qName)
		Expect(d).NotTo(BeNil())
		Expect(d.Body).To(MatchJSON(`{
			"domains": [
				"foo-bar-express.rise.cloud",
				"www.foo-bar-express.com"
			]
		}`))
	})

	Context("when skip_webroot_upload is true", func() {
		assertMetaDataUpload := func() {
			Expect(fakeS3.UploadCalls.Count()).To(Equal(2)) // 2 metadata files (2 domains)

			for i, domain := range []string{"foo-bar-express.rise.cloud", "www.foo-bar-express.com"} {
				uploadCall := fakeS3.UploadCalls.NthCall(1 + i)
				Expect(uploadCall).NotTo(BeNil())
				Expect(uploadCall.Arguments[0]).To(Equal(s3.BucketRegion))
				Expect(uploadCall.Arguments[1]).To(Equal(s3.BucketName))
				Expect(uploadCall.Arguments[2]).To(Equal("domains/" + domain + "/meta.json"))
				Expect(uploadCall.Arguments[4]).To(Equal("application/json"))
				Expect(uploadCall.Arguments[5]).To(Equal("public-read"))
				Expect(uploadCall.ReturnValues[0]).To(BeNil())
				Expect(uploadCall.SideEffects["uploaded_content"]).To(MatchJSON(`{
					"prefix": "a1b2c3-123"
				}`))
			}
		}

		It("only uploads metadata to S3, and publishes invalidation message to edges", func() {
			err = deployer.Work([]byte(`
				{
					"deployment_id": 123,
					"deployment_prefix": "a1b2c3",
					"project_name": "foo-bar-express",
					"domains": [
						"foo-bar-express.rise.cloud",
						"www.foo-bar-express.com"
					],
					"skip_webroot_upload": true
				}
			`))
			Expect(err).To(BeNil())

			assertMetaDataUpload()

			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{
				"domains": [
					"foo-bar-express.rise.cloud",
					"www.foo-bar-express.com"
				]
			}`))
		})

		Context("when skip_invalidation is also true", func() {
			It("only uploads metadata to s3, and does not publish invalidation message", func() {
				err = deployer.Work([]byte(`
					{
						"deployment_id": 123,
						"deployment_prefix": "a1b2c3",
						"project_name": "foo-bar-express",
						"domains": [
							"foo-bar-express.rise.cloud",
							"www.foo-bar-express.com"
						],
						"skip_webroot_upload": true,
						"skip_invalidation": true
					}
				`))

				Expect(err).To(BeNil())
				assertMetaDataUpload()

				d := testhelper.ConsumeQueue(mq, qName)
				Expect(d).To(BeNil())
			})
		})
	})
})
