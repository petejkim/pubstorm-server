package builder_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
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
		dm   *domain.Domain
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
		proj = factories.Project(db, u, "foo-bar")
		depl = factories.Deployment(db, proj, u, deployment.StatePendingBuild)
		dm = factories.Domain(db, proj, "www.foo-bar.com")
	})

	AfterEach(func() {
		builder.S3 = origS3
	})

	assertUpload := func(nthUpload int, uploadPath string) {
		uploadCall := fakeS3.UploadCalls.NthCall(nthUpload)
		Expect(uploadCall).NotTo(BeNil())
		Expect(uploadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
		Expect(uploadCall.Arguments[1]).To(Equal(s3client.BucketName))
		Expect(uploadCall.Arguments[2]).To(Equal(uploadPath))
		Expect(uploadCall.Arguments[4]).To(Equal(""))
		Expect(uploadCall.Arguments[5]).To(Equal("private"))
		Expect(uploadCall.ReturnValues[0]).To(BeNil())
	}

	assertCleanTempFile := func(prefixID string) {
		files, _ := ioutil.ReadDir("/tmp")
		for _, f := range files {
			Expect(f.Name()).ToNot(ContainSubstring(prefixID))
		}
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

		assertUpload(
			1,
			"deployments/"+depl.PrefixID()+"/optimized-bundle.tar.gz",
		)

		// Verify all contents
		uploadCall := fakeS3.UploadCalls.NthCall(1)
		uploadedContent, ok := uploadCall.SideEffects["uploaded_content"].([]byte)
		Expect(ok).To(BeTrue())
		buf := bytes.NewBuffer(uploadedContent)

		gr, err := gzip.NewReader(buf)
		Expect(err).To(BeNil())
		defer gr.Close()
		tr := tar.NewReader(gr)

		optimizedBundlePath := "../../testhelper/fixtures/optimized_website"
		var optimizedFileNames []string
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			Expect(err).To(BeNil())

			if hdr.FileInfo().IsDir() {
				continue
			}

			sourceContent, err := ioutil.ReadAll(tr)
			Expect(err).To(BeNil())
			if strings.HasPrefix(hdr.Name, "sitemap") {
				if hdr.Name == "sitemap.xml" {
					domainNames, err := proj.DomainNames(db)
					Expect(err).To(BeNil())
					for _, domainName := range domainNames {
						Expect(sourceContent).To(ContainSubstring(fmt.Sprintf("<loc>http://%s", domainName)))
					}
				}

				if hdr.Name == "sitemap/sitemap-foo-bar-risecloud-dev.xml" {
					Expect(sourceContent).To(ContainSubstring(fmt.Sprintf("<loc>foo-bar.risecloud.dev/</loc>")))
				}
			} else {
				targetContent, err := ioutil.ReadFile(filepath.Join(optimizedBundlePath, hdr.Name))
				Expect(err).To(BeNil())
				Expect(hex.EncodeToString(targetContent)).To(Equal(hex.EncodeToString(sourceContent)))
			}

			optimizedFileNames = append(optimizedFileNames, hdr.Name)
		}

		Expect(optimizedFileNames).To(ConsistOf([]string{
			"images/rick-astley.jpg",
			"images/astley.jpg",
			"index.html",
			"js/app.js",
			"css/app.css",
			"sitemap.xml",
			"sitemap/sitemap-foo-bar-risecloud-dev.xml",
			"sitemap/sitemap-www-foo-bar-com.xml",
		}))

		// it should publish deploy message
		d := testhelper.ConsumeQueue(mq, queues.Deploy)
		Expect(d).NotTo(BeNil())
		Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
			"deployment_id": %d,
			"skip_webroot_upload": false,
			"skip_invalidation": false,
			"use_raw_bundle": false
		}`, depl.ID)))

		// it should update deployment's state to deployed
		err = db.First(depl, depl.ID).Error
		Expect(err).To(BeNil())

		Expect(depl.ErrorMessage).To(BeNil())
		Expect(depl.State).To(Equal(deployment.StatePendingDeploy))

		assertCleanTempFile(depl.PrefixID())
	})

	Context("when the deployment uses a raw bundle from a previous deployment", func() {
		var (
			bun   *rawbundle.RawBundle
			depl2 *deployment.Deployment
		)

		BeforeEach(func() {
			bun = factories.RawBundle(db, proj)

			depl = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				State:       deployment.StateDeployed,
				RawBundleID: &bun.ID,
			})

			depl2 = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
				State:       deployment.StatePendingBuild,
				RawBundleID: &bun.ID, // Use same bundle as previous deployment.
			})
		})

		It("fetches (and uses) the raw bundle of that previous deployment", func() {
			// mock download
			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
			Expect(err).To(BeNil())

			err = builder.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl2.ID)))
			Expect(err).To(BeNil())

			// it should download raw bundle from s3
			Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
			downloadCall := fakeS3.DownloadCalls.NthCall(1)
			Expect(downloadCall).NotTo(BeNil())
			Expect(downloadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(downloadCall.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(downloadCall.Arguments[2]).To(Equal(bun.UploadedPath))
			Expect(downloadCall.ReturnValues[0]).To(BeNil())
		})

		Context("when the raw bundle has been deleted", func() {
			BeforeEach(func() {
				err := db.Delete(bun).Error
				Expect(err).To(BeNil())
			})

			It("fetches the raw bundle from the deployment's prefix directory", func() {
				// mock download
				fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/website.tar.gz")
				Expect(err).To(BeNil())

				err = builder.Work([]byte(fmt.Sprintf(`{
					"deployment_id": %d
				}`, depl2.ID)))
				Expect(err).To(BeNil())

				// it should download raw bundle from s3
				Expect(fakeS3.DownloadCalls.Count()).To(Equal(1))
				downloadCall := fakeS3.DownloadCalls.NthCall(1)
				Expect(downloadCall).NotTo(BeNil())
				Expect(downloadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(downloadCall.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(downloadCall.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s/raw-bundle.tar.gz", depl2.PrefixID())))
				Expect(downloadCall.ReturnValues[0]).To(BeNil())
			})
		})
	})

	Context("when the deployment is in an expected state", func() {
		It("returns an error if the deployment is not in `pending_build` state", func() {
			depl.State = deployment.StateUploaded
			Expect(db.Save(depl).Error).To(BeNil())

			err = builder.Work([]byte(fmt.Sprintf(`{ "deployment_id": %d }`, depl.ID)))
			Expect(err).NotTo(BeNil())

			Expect(fakeS3.DownloadCalls.Count()).To(Equal(0))
			Expect(fakeS3.UploadCalls.Count()).To(Equal(0))

			d := testhelper.ConsumeQueue(mq, queues.Deploy)
			Expect(d).To(BeNil())

			assertCleanTempFile(depl.PrefixID())
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

			assertCleanTempFile(depl.PrefixID())
		})
	})

	Context("when the optimizer times out", func() {
		var optimizerCmd *exec.Cmd

		BeforeEach(func() {
			builder.OptimizerCmd = func(srcDir string, domainNames []string) *exec.Cmd {
				optimizerCmd = exec.Command("sleep", "60")
				return optimizerCmd
			}
			builder.OptimizerTimeout = 100 * time.Millisecond

			fakeS3.DownloadContent, err = ioutil.ReadFile("../../testhelper/fixtures/malformed-website.tar.gz")
			Expect(err).To(BeNil())
		})

		It("kills the optimize process", func() {
			done := make(chan struct{})
			errCh := make(chan error)
			go func() {
				err = builder.Work([]byte(fmt.Sprintf(`{
					"deployment_id": %d
				}`, depl.ID)))

				if err != nil {
					errCh <- err
				}
				done <- struct{}{}
			}()

			select {
			case <-done:
				time.Sleep(50 * time.Millisecond)
				Expect(optimizerCmd.ProcessState.String()).To(Equal("signal: killed"))
			case err := <-errCh:
				Expect(err).To(BeNil())
			case <-time.After(150 * time.Millisecond):
				Fail("timed out on optimizer")
			}

			assertCleanTempFile(depl.PrefixID())
		})

		It("sets `UseRawBundle` to true in a deploy message", func() {
			err = builder.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl.ID)))
			Expect(err).To(BeNil())

			// it should publish deploy message
			d := testhelper.ConsumeQueue(mq, queues.Deploy)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"deployment_id": %d,
				"skip_webroot_upload": false,
				"skip_invalidation": false,
				"use_raw_bundle": true
			}`, depl.ID)))

			// it should update deployment's state to deployed
			err = db.First(depl, depl.ID).Error
			Expect(err).To(BeNil())

			Expect(depl.State).To(Equal(deployment.StatePendingDeploy))

			assertCleanTempFile(depl.PrefixID())
		})

		It("updates `error_message` in deployments table", func() {
			err = builder.Work([]byte(fmt.Sprintf(`{
				"deployment_id": %d
			}`, depl.ID)))
			Expect(err).To(BeNil())

			Expect(db.First(depl, depl.ID).Error).To(BeNil())
			Expect(depl.ErrorMessage).NotTo(BeNil())
			Expect(*depl.ErrorMessage).To(Equal(builder.ErrOptimizerTimeout.Error()))

			assertCleanTempFile(depl.PrefixID())
		})
	})
})
