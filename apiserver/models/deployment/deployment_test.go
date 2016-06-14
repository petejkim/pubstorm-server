package deployment_test

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "deployment")
}

var _ = Describe("Deployment", func() {
	var (
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("PrevousCompletedDeployment()", func() {
		var (
			d1 *deployment.Deployment
			d2 *deployment.Deployment
			d3 *deployment.Deployment
			d4 *deployment.Deployment
		)

		BeforeEach(func() {
			u := factories.User(db)
			proj := factories.Project(db, u)
			d1 = factories.Deployment(db, proj, u, deployment.StateDeployed)
			d2 = factories.Deployment(db, proj, u, deployment.StatePendingDeploy)
			d3 = factories.Deployment(db, proj, u, deployment.StateDeployed)
			Expect(db.Model(d3).Update("deployed_at", d1.DeployedAt.Add(-time.Minute)).Error).To(BeNil())
			d4 = factories.Deployment(db, proj, u, deployment.StateDeployed)
		})

		It("returns previous completed deployment", func() {
			prevDepl, err := d4.PreviousCompletedDeployment(db)
			Expect(err).To(BeNil())
			Expect(prevDepl.ID).To(Equal(d1.ID))
		})

		It("returns nil if previous completed deployment does not exist", func() {
			prevDepl, err := d3.PreviousCompletedDeployment(db)
			Expect(err).To(BeNil())
			Expect(prevDepl).To(BeNil())
		})

		It("returns nil if current deployment was not deployed", func() {
			prevDepl, err := d2.PreviousCompletedDeployment(db)
			Expect(err).To(BeNil())
			Expect(prevDepl).To(BeNil())
		})
	})

	Describe("AllCompletedDeployments()", func() {
		var (
			proj *project.Project

			d1 *deployment.Deployment
			d2 *deployment.Deployment
			d3 *deployment.Deployment
		)

		BeforeEach(func() {
			u := factories.User(db)
			proj = factories.Project(db, u)
			d1 = factories.Deployment(db, proj, u, deployment.StateDeployed)
			d2 = factories.Deployment(db, proj, u, deployment.StatePendingDeploy)
			d3 = factories.Deployment(db, proj, u, deployment.StateDeployed)
		})

		It("returns completed deployments sort by deployed_at", func() {
			depls, err := deployment.AllCompletedDeployments(db, proj.ID)
			Expect(err).To(BeNil())

			Expect(depls).To(HaveLen(2))
			Expect(depls[0].ID).To(Equal(d3.ID))
			Expect(depls[1].ID).To(Equal(d1.ID))
		})
	})

	Describe("UpdateState()", func() {
		var (
			d *deployment.Deployment
		)

		BeforeEach(func() {
			u := factories.User(db)
			proj := factories.Project(db, u)
			d = factories.Deployment(db, proj, u, deployment.StatePendingDeploy)
		})

		It("updates state", func() {
			err := d.UpdateState(db, deployment.StateUploaded)
			Expect(err).To(BeNil())

			Expect(d.State).To(Equal(deployment.StateUploaded))
			Expect(d.DeployedAt).To(BeNil())
			Expect(d.ErrorMessage).To(BeNil())
		})

		It("updates state and deployed_at if updates to deployed state", func() {
			err := d.UpdateState(db, deployment.StateDeployed)
			Expect(err).To(BeNil())

			Expect(d.State).To(Equal(deployment.StateDeployed))
			Expect(d.DeployedAt).NotTo(BeNil())
			Expect(d.DeployedAt.Unix()).To(BeNumerically("~", time.Now().Unix(), 1))
			Expect(d.ErrorMessage).To(BeNil())
		})

		It("updates state and error_message if updates to build_failed state", func() {
			msg := "You did something wrong"
			d.ErrorMessage = &msg
			err := d.UpdateState(db, deployment.StateBuildFailed)
			Expect(err).To(BeNil())

			Expect(d.State).To(Equal(deployment.StateBuildFailed))
			Expect(d.DeployedAt).To(BeNil())
			Expect(d.ErrorMessage).NotTo(BeNil())
			Expect(*d.ErrorMessage).To(Equal(msg))
		})
	})
})
