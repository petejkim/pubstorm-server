package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"

	. "github.com/onsi/gomega"
)

var domainN = 0

func Domain(db *gorm.DB, proj *project.Project, domainNames ...string) (d *domain.Domain) {
	if proj == nil {
		proj = Project(db, nil)
	}

	if domainNames == nil {
		domainN++

		d = &domain.Domain{
			ProjectID: proj.ID,
			Name:      fmt.Sprintf("www.dom%d.com", domainN),
		}
		err := db.Create(d).Error
		Expect(err).To(BeNil())
	} else {
		for _, domName := range domainNames {
			d = &domain.Domain{
				ProjectID: proj.ID,
				Name:      domName,
			}
			err := db.Create(d).Error
			Expect(err).To(BeNil())
		}
	}

	// returns only the last domain created
	return d
}
