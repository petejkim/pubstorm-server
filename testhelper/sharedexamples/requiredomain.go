package sharedexamples

import (
	"bytes"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func ItRequiresDomain(
	varFn func() (*gorm.DB, *project.Project, *domain.Domain),
	reqFn func() *http.Response,
	assertFn func(),
) {
	var (
		db   *gorm.DB
		proj *project.Project
		dm   *domain.Domain

		res *http.Response
	)

	BeforeEach(func() {
		db, proj, dm = varFn()
	})

	Context("when the domain does not exist", func() {
		BeforeEach(func() {
			Expect(db.Delete(dm).Error).To(BeNil())
			res = reqFn()
		})

		It("returns 404 not found", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			Expect(b.String()).To(MatchJSON(`{
				"error": "not_found",
				"error_description": "domain could not be found"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})

	Context("when the domain does not belong to project", func() {
		BeforeEach(func() {
			proj2 := factories.Project(db, nil)
			err := db.Model(dm).Update("project_id", proj2.ID).Error
			Expect(err).To(BeNil())
			res = reqFn()
		})

		It("returns 404 not found", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			Expect(b.String()).To(MatchJSON(`{
				"error": "not_found",
				"error_description": "domain could not be found"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})
}
