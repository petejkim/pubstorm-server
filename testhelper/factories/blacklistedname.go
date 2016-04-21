package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedname"

	. "github.com/onsi/gomega"
)

var blacklistedNameN = 0

func BlacklistedName(db *gorm.DB, name string) (dpn *blacklistedname.BlacklistedName) {
	if name == "" {
		name = fmt.Sprintf("blacklisted-name-%04d", blacklistedNameN)
	}

	dpn = &blacklistedname.BlacklistedName{
		Name: name,
	}

	err := db.Create(dpn).Error
	Expect(err).To(BeNil())

	return dpn
}
