package deployer_test

import (
	"io/ioutil"
	"testing"

	"github.com/nitrous-io/rise-server/common"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/workers/deployer/deployer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
	)

	BeforeEach(func() {
		origS3 = common.S3
		fakeS3 = &fake.FileTransfer{}
		deployer.S3 = fakeS3
	})

	AfterEach(func() {
		deployer.S3 = origS3
	})

	It("fetches the raw bundle from S3 and uploads to S3", func() {
		// mock download
		fakeS3.DownloadContent, err = ioutil.ReadFile("../../../testhelper/fixtures/website.tar.gz")
		Expect(err).To(BeNil())

		err = deployer.Work([]byte(`
			{
				"deployment_id": 123,
				"deployment_prefix": "a1b2c3",
				"project_name": "foo-bar-express",
				"domain": "foo-bar-express.rise.cloud"
			}
		`))
		Expect(err).To(BeNil())

		Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
		downloadCall := fakeS3.DownloadCalls.NthCall(1)
		Expect(downloadCall).NotTo(BeNil())
		Expect(downloadCall.Arguments[0]).To(Equal(deployer.S3BucketRegion))
		Expect(downloadCall.Arguments[1]).To(Equal(deployer.S3BucketName))
		Expect(downloadCall.Arguments[2]).To(Equal("deployments/a1b2c3-123/raw-bundle.tar.gz"))
		Expect(downloadCall.ReturnValues[0]).To(BeNil())

		Expect(fakeS3.UploadCalls.Count()).To(Equal(5)) // 4 files + metadata file

		uploads := []string{
			"images/rick-astley.jpg",
			"index.html",
			"js/app.js",
			"css/app.css",
		}

		for i, upload := range uploads {
			uploadCall := fakeS3.UploadCalls.NthCall(i + 1)
			Expect(uploadCall).NotTo(BeNil())
			Expect(uploadCall.Arguments[0]).To(Equal(deployer.S3BucketRegion))
			Expect(uploadCall.Arguments[1]).To(Equal(deployer.S3BucketName))
			Expect(uploadCall.Arguments[2]).To(Equal("deployments/a1b2c3-123/webroot/" + upload))
			Expect(uploadCall.Arguments[4]).To(Equal("private"))
			Expect(uploadCall.ReturnValues[0]).To(BeNil())

			data, err := ioutil.ReadFile("../../../testhelper/fixtures/website/" + upload)
			Expect(err).To(BeNil())
			Expect(uploadCall.SideEffects["uploaded_content"]).To(Equal(data))
		}

		uploadCall := fakeS3.UploadCalls.NthCall(5)
		Expect(uploadCall).NotTo(BeNil())
		Expect(uploadCall.Arguments[0]).To(Equal(deployer.S3BucketRegion))
		Expect(uploadCall.Arguments[1]).To(Equal(deployer.S3BucketName))
		Expect(uploadCall.Arguments[2]).To(Equal("domains/foo-bar-express.rise.cloud/meta.json"))
		Expect(uploadCall.Arguments[4]).To(Equal("public-read"))
		Expect(uploadCall.ReturnValues[0]).To(BeNil())
		Expect(uploadCall.SideEffects["uploaded_content"]).To(MatchJSON(`
			{
				"webroot": "deployments/a1b2c3-123/webroot"
			}
		`))
	})
})
