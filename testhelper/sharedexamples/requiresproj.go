package sharedexamples

import (
	"bytes"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func ItRequiresProject(
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

	Context("when the project does not exist", func() {
		BeforeEach(func() {
			Expect(db.Delete(proj).Error).To(BeNil())
			res = reqFn()
		})

		It("returns 404 not found", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			Expect(b.String()).To(MatchJSON(`{
				"error": "not_found",
				"error_description": "project could not be found"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})

	Context("when the project does not belong to current user", func() {
		BeforeEach(func() {
			u2 := factories.User(db)
			err := db.Model(proj).Update("user_id", u2.ID).Error
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
				"error_description": "project could not be found"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})
}
