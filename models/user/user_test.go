package user_test

import (
	"strings"
	"testing"

	"github.com/nitrous-io/rise-server/models/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "user")
}

var _ = Describe("User", func() {
	Describe("Validate()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
		})

		It("validates email", func() {
			Expect(u.Validate()).To(BeNil())

			u.Email = ""
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["email"]).To(Equal("is required"))

			u.Email = "harry.potter@g"
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["email"]).To(Equal("is invalid"))
		})

		It("validates password", func() {
			Expect(u.Validate()).To(BeNil())

			u.Password = ""
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is required"))

			u.Password = "12345"
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is too short (min. 6 characters)"))

			u.Password = strings.Repeat("A", 73)
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is too long (max. 72 characters)"))
		})
	})
})
