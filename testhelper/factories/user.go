package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	. "github.com/onsi/gomega"
)

var userN = 0

func User(db *gorm.DB) (u *user.User) {
	userN++

	u = &user.User{
		Email:    fmt.Sprintf("foo%d@example.com", userN),
		Password: "foobar",
	}
	err := u.Insert(db)
	Expect(err).To(BeNil())

	err = db.Model(&u).Update("confirmed_at", gorm.Expr("now()")).Error
	Expect(err).To(BeNil())

	return u
}
