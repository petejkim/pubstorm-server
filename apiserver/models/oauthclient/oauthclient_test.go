package oauthclient_test

import (
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/testhelper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oauthclient")
}

var _ = Describe("OauthClient", func() {
	var (
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("Authenticate()", func() {
		var c *oauthclient.OauthClient

		BeforeEach(func() {
			c = &oauthclient.OauthClient{
				ClientID:     "foo",
				ClientSecret: "foobarbazqux",
			}
			err = db.Create(c).Error
			Expect(err).To(BeNil())
			Expect(c.ID).NotTo(BeZero())
		})

		Context("when the crendentials are valid", func() {
			It("returns user", func() {
				c2, err := oauthclient.Authenticate(db, c.ClientID, c.ClientSecret)
				Expect(c2).NotTo(BeNil())
				Expect(c2.ID).To(Equal(c.ID))
				Expect(c2.ClientID).To(Equal(c.ClientID))
				Expect(err).To(BeNil())
			})
		})

		Context("when the crendentials are invalid", func() {
			It("returns nil", func() {
				c2, err := oauthclient.Authenticate(db, c.ClientID, c.ClientSecret+"x")
				Expect(c2).To(BeNil())
				Expect(err).To(BeNil())
			})
		})
	})
})
