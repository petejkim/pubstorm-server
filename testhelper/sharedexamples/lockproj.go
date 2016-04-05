package sharedexamples

import (
	"bytes"
	"net/http"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func ItLocksProject(
	varFn func() (*gorm.DB, *project.Project),
	reqFn func() *http.Response,
	assertFn func(),
) {
	var (
		db   *gorm.DB
		proj *project.Project

		res *http.Response
	)

	BeforeEach(func() {
		db, proj = varFn()
	})

	Context("when the project is locked", func() {
		var (
			deploymentCnt int
		)

		BeforeEach(func() {
			currentTime := time.Now()
			proj.LockedAt = &currentTime
			db.Save(proj)
		})

		It("returns 423 with locked", func() {
			Expect(db.Model(deployment.Deployment{}).Count(&deploymentCnt).Error).To(BeNil())
			res = reqFn()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(423))
			Expect(b.String()).To(MatchJSON(`{
						"error": "locked",
						"error_description": "project is locked"
					}`))

			var currentDeploymentCnt int
			Expect(db.Model(deployment.Deployment{}).Count(&currentDeploymentCnt).Error).To(BeNil())
			Expect(currentDeploymentCnt).To(Equal(deploymentCnt))

			if assertFn != nil {
				assertFn()
			}
		})

		It("does not unlock the project", func() {
			reqFn()
			var updatedProj project.Project
			Expect(db.First(&updatedProj, proj.ID).Error).To(BeNil())
			Expect(updatedProj.LockedAt).NotTo(BeNil())
		})
	})

	Context("when the project is not locked", func() {
		It("does not leave the project as locked", func() {
			reqFn()
			var updatedProj project.Project
			Expect(db.First(&updatedProj, proj.ID).Error).To(BeNil())
			Expect(updatedProj.LockedAt).To(BeNil())
		})
	})
}
