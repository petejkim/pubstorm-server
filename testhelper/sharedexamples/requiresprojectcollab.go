package sharedexamples

import (
	"bytes"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func ItRequiresProjectCollab(
	varFn func() (*gorm.DB, *user.User, *project.Project),
	reqFn func() *http.Response,
	assertFn func(),
) {
	var (
		db   *gorm.DB
		u    *user.User
		proj *project.Project

		res *http.Response
	)

	BeforeEach(func() {
		db, u, proj = varFn()
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
		})

		Context("when user is not a collaborator of the project", func() {
			It("returns 404 not found", func() {
				res = reqFn()

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

		Context("when user is a collaborator of the project", func() {
			BeforeEach(func() {
				err := proj.AddCollaborator(db, u)
				Expect(err).To(BeNil())
			})

			It("does not respond with project could not be found", func() {
				res = reqFn()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(b.String()).NotTo(MatchJSON(`{
					"error": "not_found",
					"error_description": "project could not be found"
				}`))

				// We do not call assertFn here because that should only be run
				// when the user is NOT a project owner or collaborator.
			})
		})
	})
}
