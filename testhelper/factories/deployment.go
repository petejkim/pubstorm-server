package factories

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/gomega"
)

func Deployment(db *gorm.DB, proj *project.Project, u *user.User, state string) (d *deployment.Deployment) {
	if u == nil {
		u = User(db)
	}

	if proj == nil {
		proj = Project(db, u)
	}

	d = &deployment.Deployment{
		ProjectID: proj.ID,
		UserID:    u.ID,
		State:     state,
	}

	err := db.Create(d).Error
	Expect(err).To(BeNil())

	return d
}
