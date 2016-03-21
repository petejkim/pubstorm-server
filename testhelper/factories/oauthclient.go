package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	. "github.com/onsi/gomega"
)

var oauthClientN = 0

func OauthClient(db *gorm.DB) (oc *oauthclient.OauthClient) {
	oauthClientN++

	oc = &oauthclient.OauthClient{
		Email:        fmt.Sprintf("foo%d@example.com", oauthClientN),
		Name:         fmt.Sprintf("FooClient%d", oauthClientN),
		Organization: "FooCorp",
	}
	err := db.Create(oc).Error
	Expect(err).To(BeNil())

	return oc
}
