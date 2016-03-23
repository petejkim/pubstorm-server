package project

import (
	"regexp"
	"sort"

	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"

	"github.com/jinzhu/gorm"
)

var projectNameRe = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9\-]{1,61}[A-Za-z0-9]\z`)

type Project struct {
	gorm.Model

	Name   string
	UserID uint
}

// Validates Project, if there are invalid fields, it returns a map of
// <field, errors> and returns nil if valid
func (p *Project) Validate() map[string]string {
	errors := map[string]string{}

	if p.Name == "" {
		errors["name"] = "is required"
	} else if len(p.Name) < 3 {
		errors["name"] = "is too short (min. 3 characters)"
	} else if len(p.Name) > 63 {
		errors["name"] = "is too long (max. 63 characters)"
	} else if !projectNameRe.MatchString(p.Name) {
		errors["name"] = "is invalid"
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

// Returns a struct that can be converted to JSON
func (p *Project) AsJSON() interface{} {
	return struct {
		Name string `json:"name"`
	}{
		p.Name,
	}
}

// get list of domain names for this project
func (p *Project) DomainNames() ([]string, error) {
	db, err := dbconn.DB()
	if err != nil {
		return nil, err
	}

	doms := []*domain.Domain{}
	if err := db.Where("project_id = ?", p.ID).Find(&doms).Error; err != nil {
		return nil, err
	}

	domNames := make([]string, len(doms)+1)
	for i, dom := range doms {
		domNames[i+1] = dom.Name
	}
	sort.Sort(sort.StringSlice(domNames))
	domNames[0] = p.Name + "." + common.DefaultDomain

	return domNames, nil
}

// Find project by name
func FindByName(name string) (proj *Project, err error) {
	db, err := dbconn.DB()
	if err != nil {
		return nil, err
	}

	proj = &Project{}
	q := db.Where("name = ?", name).First(proj)
	if err = q.Error; err != nil {
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return proj, nil
}
