package blacklistedname_test

import (
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedname"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "blacklistedname")
}

var _ = Describe("BlacklistedName", func() {
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
				factories.BlacklistedName(db, "foo-bar-express")
			})

			It("returns true", func() {
				blacklisted, err := blacklistedname.IsBlacklisted(db, "foo-bar-express")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeTrue())
			})
		})

		Context("the name does not exist in table", func() {
			It("returns false", func() {
				blacklisted, err := blacklistedname.IsBlacklisted(db, "foo-bar-express")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeFalse())
			})
		})
	})
})
