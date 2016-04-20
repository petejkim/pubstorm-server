package factories

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/gomega"
)

func Collab(db *gorm.DB, p *project.Project, u *user.User) *collab.Collab {
	if u == nil {
		u = User(db)
	}

	if p == nil {
		p = Project(db, u)
	}

	c := &collab.Collab{
		UserID:    u.ID,
		ProjectID: p.ID,
	}

	err := db.Create(c).Error
	Expect(err).To(BeNil())

	return c
}
