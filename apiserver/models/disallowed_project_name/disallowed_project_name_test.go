package disallowed_project_name_test

import (
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/disallowed_project_name"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "disallowed_project_name")
}

var _ = Describe("DisallowedProjectName", func() {
	var (
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("IsBlacklisted()", func() {
		Context("the name exists in table", func() {
			BeforeEach(func() {
				factories.DisallowedProjectName(db, "foo-bar-express")
			})

			It("returns true", func() {
				blacklisted, err := disallowed_project_name.IsBlacklisted(db, "foo-bar-express")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeTrue())
			})
		})

		Context("the name does not exist in table", func() {
			It("returns false", func() {
				blacklisted, err := disallowed_project_name.IsBlacklisted(db, "foo-bar-express")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeFalse())
			})
		})
	})
})
