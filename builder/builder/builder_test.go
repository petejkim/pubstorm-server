package builder_test

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/builder/builder"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
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
	RunSpecs(t, "builder")
}

var _ = Describe("Builder", func() {
	var (
		fakeS3 *fake.S3
		origS3 filetransfer.FileTransfer
		err    error

		db *gorm.DB
		mq *amqp.Connection

		u    *user.User
		proj *project.Project
		depl *deployment.Deployment
	)

	BeforeEach(func() {
		origS3 = s3client.S3
		fakeS3 = &fake.S3{}
		builder.S3 = fakeS3

		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
		testhelper.DeleteQueue(mq, queues.All...)

		u = factories.User(db)
		proj = factories.Project(db, u)
		depl = factories.Deployment(db, proj, u, deployment.StatePendingBuild)
	})

	AfterEach(func() {
		builder.S3 = origS3
	})

	assertUpload := func(nthUpload int, uploadPath string, content []byte) {
		uploadCall := fakeS3.UploadCalls.NthCall(nthUpload)
		Expect(uploadCall).NotTo(BeNil())
		Expect(uploadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
		Expect(uploadCall.Arguments[1]).To(Equal(s3client.BucketName))
		Expect(uploadCall.Arguments[2]).To(Equal(uploadPath))
		Expect(uploadCall.Arguments[4]).To(Equal(""))
		Expect(uploadCall.Arguments[5]).To(Equal("private"))
		Expect(uploadCall.ReturnValues[0]).To(BeNil())
	}

	It("fetches the raw bundle from S3, optimize assets, compress and upload to S3 and publish a message to 'deploy' queue", func() {
		// mock download
		fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
		Expect(err).To(BeNil())

		err = builder.Work([]byte(fmt.Sprintf(`{
			"deployment_id": %d
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

		// it should upload optimized assets as a tar-gzipped file.
		Expect(fakeS3.UploadCalls.Count()).To(Equal(1))

		data, err := ioutil.ReadFile("../../testhelper/fixtures/optimized-website.tar.gz")
		Expect(err).To(BeNil())

		assertUpload(
			1,
			"deployments/"+depl.PrefixID()+"/optimized-bundle.tar.gz",
			data,
		)

		// it should publish deploy message
		d := testhelper.ConsumeQueue(mq, queues.Deploy)
		Expect(d).NotTo(BeNil())
		Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
			"deployment_id": %d,
			"skip_webroot_upload": false,
			"skip_invalidation": false
		}`, depl.ID)))

		// it should update deployment's state to deployed
		err = db.First(depl, depl.ID).Error
		Expect(err).To(BeNil())

		Expect(depl.ErrorMessage).To(BeNil())
		Expect(depl.State).To(Equal(deployment.StatePendingDeploy))
	})

	Context("the deployment is not in expected state", func() {
		It("returns an error if the deployment is not in `pending_build` state", func() {
			depl.State = deployment.StateUploaded
			Expect(db.Save(depl).Error).To(BeNil())

			err = builder.Work([]byte(fmt.Sprintf(`{ "deployment_id": %d }`, depl.ID)))
			Expect(err).NotTo(BeNil())

			Expect(fakeS3.DownloadCalls.Count()).To(Equal(0))
			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

			d := testhelper.ConsumeQueue(mq, queues.Deploy)
			Expect(d).To(BeNil())
		})
	})

	Context("when there are error messages from optimizer", func() {
		It("updates `error_message` in deployments table", func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/malformed-website.tar.gz")
			Expect(err).To(BeNil())

			err = builder.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl.ID)))
			Expect(err).To(BeNil())

			Expect(db.First(depl, depl.ID).Error).To(BeNil())
			Expect(depl.ErrorMessage).NotTo(BeNil())
			Expect(*depl.ErrorMessage).To(ContainSubstring(`index.html:Parse Error: <h1Give You Up</h1>    <iframe width="420" height="315" src="https://www.youtube.com/embed/dQw4w9WgXcQ" frameborder="0" allowfullscreen></iframe>    <img src="images/rick-astley.jpg" title="I love you">  </body  /html>`))
			Expect(*depl.ErrorMessage).To(ContainSubstring(`js/app.js:11:Unexpected token`))
			Expect(*depl.ErrorMessage).To(ContainSubstring(`css/app.css:Missing '}' after '  __ESCAPED_FREE_TEXT_CLEAN_CSS0____ESCAPED_SOURCE_END_CLEAN_CSS__'. Ignoring.`))
			Expect(*depl.ErrorMessage).To(ContainSubstring(`images/astley.jpg:Failed to optimize`))
		})
	})
})
