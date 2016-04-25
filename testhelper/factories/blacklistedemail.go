package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedemail"

	. "github.com/onsi/gomega"
)

var blacklistedEmailN = 0

func BlacklistedEmail(db *gorm.DB, email string) (be *blacklistedemail.BlacklistedEmail) {
	if email == "" {
		email = fmt.Sprintf("blacklisted-email-%04d.com", blacklistedEmailN)
	}

	be = &blacklistedemail.BlacklistedEmail{
		Email: email,
	}

	err := db.Create(be).Error
	Expect(err).To(BeNil())

	return be
}
