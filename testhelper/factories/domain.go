package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"

	. "github.com/onsi/gomega"
)

var domainN = 0

func Domain(db *gorm.DB, proj *project.Project) (d *domain.Domain) {
	domainN++

	if proj == nil {
		proj = Project(db, nil)
	}

	d = &domain.Domain{
		ProjectID: proj.ID,
		Name:      fmt.Sprintf("www.dom%d.com", domainN),
	}

	err := db.Create(d).Error
	Expect(err).To(BeNil())

	return d
}
