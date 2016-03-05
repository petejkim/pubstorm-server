package factories

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/models/oauthclient"
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/user"
	. "github.com/onsi/gomega"
)

func AuthDuo(db *gorm.DB) (u *user.User, oc *oauthclient.OauthClient) {
	return User(db), OauthClient(db)
}

func AuthTrio(db *gorm.DB) (u *user.User, oc *oauthclient.OauthClient, t *oauthtoken.OauthToken) {
	u, oc = AuthDuo(db)

	t = &oauthtoken.OauthToken{
		UserID:        u.ID,
		OauthClientID: oc.ID,
	}

	err := db.Create(t).Error
	Expect(err).To(BeNil())

	return u, oc, t
}
