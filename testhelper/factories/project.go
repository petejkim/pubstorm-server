package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/gomega"
)

var projectN = 0

func Project(db *gorm.DB, u *user.User, name ...string) (proj *project.Project) {
	if u == nil {
		u = User(db)
	}

	var pName string
	if len(name) > 0 {
		pName = name[0]
	} else {
		projectN++
		pName = fmt.Sprintf("project%04d", projectN)
	}

	proj = &project.Project{
		UserID:               u.ID,
		Name:                 pName,
		DefaultDomainEnabled: true,
	}

	err := db.Create(proj).Error
	Expect(err).To(BeNil())

	return proj
}
