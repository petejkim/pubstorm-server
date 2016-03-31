package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/disallowed_project_name"

	. "github.com/onsi/gomega"
)

var disallowedProjectNameN = 0

func DisallowedProjectName(db *gorm.DB, name string) (dpn *disallowed_project_name.DisallowedProjectName) {
	if name == "" {
		name = fmt.Sprintf("disallowed-project-name-%d", disallowedProjectNameN)
	}

	dpn = &disallowed_project_name.DisallowedProjectName{
		Name: name,
	}

	err := db.Create(dpn).Error
	Expect(err).To(BeNil())

	return dpn
}
