package factories

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/gomega"
)

func Deployment(db *gorm.DB, proj *project.Project, u *user.User, state string) *deployment.Deployment {
	return DeploymentWithAttrs(db, proj, u, deployment.Deployment{
		State: state,
	})
}

func DeploymentWithAttrs(db *gorm.DB, proj *project.Project, u *user.User, attrs deployment.Deployment) *deployment.Deployment {
	d := &attrs

	if u == nil {
		u = User(db)
	}

	if proj == nil {
		proj = Project(db, u)
	}

	if d.UserID == 0 {
		d.UserID = u.ID
	}
	if d.ProjectID == 0 {
		d.ProjectID = proj.ID
	}

	if d.Version == 0 {
		ver, err := proj.NextVersion(db)
		Expect(err).To(BeNil())
		d.Version = ver
	}

	if d.State == deployment.StateDeployed && d.DeployedAt == nil {
		currentTime := time.Now()
		d.DeployedAt = &currentTime
	}

	err := db.Create(d).Error
	Expect(err).To(BeNil())

	return d
}
