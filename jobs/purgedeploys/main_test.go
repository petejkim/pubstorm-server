package main

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "purgedeploys")
}

var _ = Describe("purgedeploys", func() {
	var (
		fakeS3 *fake.S3
		origS3 filetransfer.FileTransfer
		err    error

		db *gorm.DB

		u                          *user.User
		proj1, proj2               *project.Project
		depl1, depl2, depl3, depl4 *deployment.Deployment
	)

	BeforeEach(func() {
		origS3 = s3client.S3
		fakeS3 = &fake.S3{}
		S3 = fakeS3

		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())

		u = factories.User(db)
		proj1 = factories.Project(db, u)
		_ = factories.Domain(db, proj1, "www.foo-bar-express.com")

		// Deployed, non-deleted deployment.
		depl1 = factories.DeploymentWithAttrs(db, proj1, u, deployment.Deployment{
			Prefix: "p1-a",
			State:  deployment.StateDeployed,
		})

		// Deleted deployment.
		depl2 = factories.DeploymentWithAttrs(db, proj1, u, deployment.Deployment{
			Prefix: "p1-b",
			State:  deployment.StateDeployed,
		})
		err = db.Delete(depl2).Error
		Expect(err).To(BeNil())

		// Purged deployment.
		depl3 = factories.DeploymentWithAttrs(db, proj1, u, deployment.Deployment{
			Prefix: "p1-c",
			State:  deployment.StateDeployed,
		})
		err = db.Delete(depl3).Error
		Expect(err).To(BeNil())
		err = db.Model(depl3).Unscoped().UpdateColumn("purged_at", time.Now()).Error
		Expect(err).To(BeNil())

		// Deleted deployment of project 2.
		depl4 = factories.DeploymentWithAttrs(db, proj2, u, deployment.Deployment{
			Prefix: "p2-a",
			State:  deployment.StateDeployed,
		})
		err = db.Delete(depl4).Error
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		S3 = origS3
	})

	Describe("findSoftDeletedDeployments()", func() {
		It("returns soft-deleted deployments that have not yet been purged", func() {
			depls, err := findSoftDeletedDeployments(db)
			Expect(err).To(BeNil())

			Expect(depls).To(HaveLen(2))

			ids := []uint{}
			for _, depl := range depls {
				ids = append(ids, depl.ID)
			}
			Expect(ids).To(ConsistOf(depl2.ID, depl4.ID))
		})
	})

	Describe("purge()", func() {
		It("deletes the deployment's files from S3", func() {
			err := purge(db, depl2)
			Expect(err).To(BeNil())

			Expect(fakeS3.DeleteAllCalls.Count()).To(Equal(1))
			deleteCall := fakeS3.DeleteAllCalls.NthCall(1)
			Expect(deleteCall).NotTo(BeNil())
			Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(deleteCall.Arguments[2]).To(Equal("deployments/" + depl2.PrefixID()))
			Expect(deleteCall.ReturnValues[0]).To(BeNil())
		})

		It("sets purged_at", func() {
			Expect(depl2.PurgedAt).To(BeNil())

			err := purge(db, depl2)
			Expect(err).To(BeNil())
			Expect(depl2.PurgedAt).NotTo(BeNil())

			err = db.Unscoped().First(depl2, depl2.ID).Error
			Expect(err).To(BeNil())
			Expect(depl2.PurgedAt).NotTo(BeNil())
		})

		It("deletes the deployment's raw bundle", func() {
			bun := factories.RawBundle(db, proj1)

			depl5 := factories.DeploymentWithAttrs(db, proj1, u, deployment.Deployment{
				Prefix:      "p1-d",
				State:       deployment.StateDeployed,
				RawBundleID: &bun.ID,
			})
			err := db.Delete(depl5).Error
			Expect(err).To(BeNil())

			err = purge(db, depl5)
			Expect(err).To(BeNil())

			err = db.Unscoped().First(depl5, depl5.ID).Error
			Expect(err).To(BeNil())

			err = db.First(bun, *depl5.RawBundleID).Error
			Expect(err).To(Equal(gorm.RecordNotFound))

			err = db.Unscoped().First(bun, *depl5.RawBundleID).Error
			Expect(err).To(BeNil())
			Expect(bun.ProjectID).To(Equal(proj1.ID))
		})
	})
})
