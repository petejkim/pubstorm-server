package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/gomega"
)

var projectN = 0

func Project(db *gorm.DB, u *user.User) (proj *project.Project) {
	projectN++

	if u == nil {
		u = User(db)
	}

	proj = &project.Project{
		UserID:               u.ID,
		Name:                 fmt.Sprintf("project%d", projectN),
		DefaultDomainEnabled: true,
	}

	err := db.Create(proj).Error
	Expect(err).To(BeNil())

	return proj
}
