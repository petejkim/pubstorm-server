package domain_test

import (
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "domain")
}

var _ = Describe("Domain", func() {
	var (
		u    *user.User
		proj *project.Project

		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u = factories.User(db)
		proj = factories.Project(db, u)
	})

	Describe("Sanitize()", func() {
		var dom *domain.Domain

		BeforeEach(func() {
			dom = &domain.Domain{
				ProjectID: proj.ID,
				Name:      "",
			}
		})

		DescribeTable("sanitizes name",
			func(name, sanitizedName string) {
				dom.Name = name
				dom.Sanitize()
				Expect(dom.Name).To(Equal(sanitizedName))
			},

			Entry(
				"add www to name with generic .com tld",
				"abc.com",
				"www.abc.com",
			),
			Entry(
				"add www to name with country code .au tld",
				"abc.au",
				"www.abc.au",
			),
			Entry(
				"add www to name with country code .com.au tld",
				"abc.com.au",
				"www.abc.com.au",
			),
			Entry(
				"add www to name with generic .co and country code .au tld",
				"abc.co.id",
				"www.abc.co.id",
			),
			Entry(
				"does not add www to generic non-apex domain",
				"www.abc.com",
				"www.abc.com",
			),
			Entry(
				"does not add www to generic .co non-apex domain",
				"www.abc.co",
				"www.abc.co",
			),
			Entry(
				"does not add www to country code non-apex domain",
				"www.abc.com.au",
				"www.abc.com.au",
			),
			Entry(
				"does not add www to generic and country code non-apex domain",
				"www.abc.co.id",
				"www.abc.co.id",
			),
			Entry(
				"does not add www to subdomain with generic non-apex domain",
				"blog.abc.co",
				"blog.abc.co",
			),
			Entry(
				"does not add www to subdomain with generic and country code non-apex domain",
				"blog.abc.co.id",
				"blog.abc.co.id",
			),
		)
	})

	Describe("Validate()", func() {
		var dom *domain.Domain

		BeforeEach(func() {
			dom = &domain.Domain{
				ProjectID: proj.ID,
				Name:      "",
			}
		})

		DescribeTable("validates name",
			func(name, nameErr string) {
				dom.Name = name
				errors := dom.Validate()

				if nameErr == "" {
					Expect(errors).To(BeNil())
				} else {
					Expect(errors).NotTo(BeNil())
					Expect(errors["name"]).To(Equal(nameErr))
				}
			},

			Entry("normal", "abc.com", ""),
			Entry("allows hyphens", "good-one.com", ""),
			Entry("allows numbers", "www.007.com", ""),
			Entry("allows multiple hyphens", "hello-world--foobar.com", ""),
			Entry("allows multiple subdomains", "this.is.an.example.com", ""),
			Entry("disallows uppercase letters", "Www.Bad-one.COM", "is invalid"),
			Entry("disallows domains beginning with a dot", ".abc.com", "is invalid"),
			Entry("disallows domains ending with a dot", "abc.com.", "is invalid"),
			Entry("disallows domains without a dot", "abc", "is invalid"),
			Entry("disallows domains with consecutive dots", "abc..com", "is invalid"),
			Entry("disallows starting with a hyphen", "-abc.com", "is invalid"),
			Entry("disallows ending with a hyphen", "abc-.com", "is invalid"),
			Entry("disallows spaces", "good one.com", "is invalid"),
			Entry("disallows special characters", "good&one.com", "is invalid"),
			Entry("disallows default domain", shared.DefaultDomain, "is invalid"),
			Entry("disallows default subdomain", "abc."+shared.DefaultDomain, "is invalid"),
			Entry("disallows multiline regex attack", "abc.com\ndef.com", "is invalid"),
			Entry("disallows names shorter than 3 characters", "co", "is too short (min. 3 characters)"),
			Entry("disallows names longer than 255 characters", strings.Repeat("a", 252)+".com", "is too long (max. 255 characters)"),
		)
	})
})
