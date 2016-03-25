package project

import (
	"regexp"
	"sort"

	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/shared"

	"github.com/jinzhu/gorm"
)

var projectNameRe = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9\-]{1,61}[A-Za-z0-9]\z`)

type Project struct {
	gorm.Model

	Name   string
	UserID uint

	ActiveDeploymentID *uint // pointer to be nullable. remember to dereference by using *ActiveDeploymentID to get actual value
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

// Returns list of domain names for this project
func (p *Project) DomainNames(db *gorm.DB) ([]string, error) {
	doms := []*domain.Domain{}
	if err := db.Where("project_id = ?", p.ID).Find(&doms).Error; err != nil {
		return nil, err
	}

	domNames := make([]string, len(doms)+1)
	for i, dom := range doms {
		domNames[i+1] = dom.Name
	}
	sort.Sort(sort.StringSlice(domNames))
	domNames[0] = p.Name + "." + shared.DefaultDomain

	return domNames, nil
}

// Find project by name
func FindByName(db *gorm.DB, name string) (proj *Project, err error) {
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

// Returns whether more domains can be added to this project
func (p *Project) CanAddDomain(db *gorm.DB) (bool, error) {
	var domainCount int
	if err := db.Model(domain.Domain{}).Where("project_id = ?", p.ID).Count(&domainCount).Error; err != nil {
		return false, err
	}

	if domainCount < shared.MaxDomainsPerProject {
		return true, nil
	}

	return false, nil
}
