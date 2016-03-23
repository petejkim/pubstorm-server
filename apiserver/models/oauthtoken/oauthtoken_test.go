package oauthtoken_test

import (
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/testhelper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oauthtoken")
}

var _ = Describe("OauthToken", func() {
	var (
		t   *oauthtoken.OauthToken
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("FindByToken()", func() {
		Context("the user exists", func() {
			BeforeEach(func() {
				c := &oauthclient.OauthClient{}
				err = db.Create(c).Error
				Expect(err).To(BeNil())

				u := &user.User{
					Email:    "harry.potter@gmail.com",
					Password: "123456",
				}
				err = u.Insert(db)
				Expect(err).To(BeNil())

				t = &oauthtoken.OauthToken{
					OauthClientID: c.ID,
					UserID:        u.ID,
				}
				err = db.Create(t).Error
				Expect(err).To(BeNil())
			})

			Context("when the token is valid", func() {
				It("returns token", func() {
					t1, err := oauthtoken.FindByToken(db, t.Token)
					Expect(err).To(BeNil())
					Expect(t1.ID).To(Equal(t.ID))
					Expect(t1.Token).To(Equal(t.Token))
				})
			})

			Context("when the token does not exist", func() {
				It("returns nil", func() {
					t1, err := oauthtoken.FindByToken(db, t.Token+"xx")
					Expect(t1).To(BeNil())
					Expect(err).To(BeNil())
				})
			})
		})
	})
})
