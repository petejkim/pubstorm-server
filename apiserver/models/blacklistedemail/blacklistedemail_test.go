package blacklistedemail_test

import (
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedemail"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "blacklistedemail")
}

var _ = Describe("BlacklistedEmail", func() {
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
		Context("the email exists in table", func() {
			BeforeEach(func() {
				factories.BlacklistedEmail(db, "foo-bar-express.com")
			})

			It("returns true", func() {
				blacklisted, err := blacklistedemail.IsBlacklisted(db, "hello@foo-bar-express.com")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeTrue())
			})
		})

		Context("the email does not exist in table", func() {
			It("returns false", func() {
				blacklisted, err := blacklistedemail.IsBlacklisted(db, "hello@foo-bar-express.com")
				Expect(err).To(BeNil())

				Expect(blacklisted).To(BeFalse())
			})
		})

		Context("the email does not have `@`", func() {
			It("returns true with an error", func() {
				blacklisted, err := blacklistedemail.IsBlacklisted(db, "foo-bar-express.com")
				Expect(err).NotTo(BeNil())

				Expect(blacklisted).To(BeTrue())
			})
		})

		Context("the email has `@` more than one", func() {
			It("returns true with an error", func() {
				blacklisted, err := blacklistedemail.IsBlacklisted(db, "hello@@@foo-bar-express.com")
				Expect(err).NotTo(BeNil())

				Expect(blacklisted).To(BeTrue())
			})
		})
	})
})
