package factories

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/models/oauthclient"
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/user"

	. "github.com/onsi/gomega"
)

func AuthDuo(db *gorm.DB) (u *user.User, oc *oauthclient.OauthClient) {
	u = &user.User{Email: "foo@example.com", Password: "foobar"}
	err := u.Insert(db)
	Expect(err).To(BeNil())

	err = db.Model(&u).Update("confirmed_at", gorm.Expr("now()")).Error
	Expect(err).To(BeNil())

	oc = &oauthclient.OauthClient{
		Email:        "foo@example.com",
		Name:         "Foo CLI",
		Organization: "FooCorp",
	}
	err = db.Create(oc).Error
	Expect(err).To(BeNil())

	return u, oc
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
