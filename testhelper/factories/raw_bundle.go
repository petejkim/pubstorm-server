package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"

	. "github.com/onsi/gomega"
)

var RawBundleN = 0

func RawBundle(db *gorm.DB, p *project.Project) (bun *rawbundle.RawBundle) {
	if p == nil {
		p = Project(db, nil)
	}

	RawBundleN++
	bun = &rawbundle.RawBundle{
		ProjectID:    p.ID,
		Checksum:     fmt.Sprintf("checksum-%d", RawBundleN),
		UploadedPath: fmt.Sprintf("/path/to/bundle-%d", RawBundleN),
	}

	err := db.Create(bun).Error
	Expect(err).To(BeNil())

	return bun
}
