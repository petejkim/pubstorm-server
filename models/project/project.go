package project

import (
	"regexp"

	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/user"

	"github.com/jinzhu/gorm"
)

var projectNameRe = regexp.MustCompile(`(?m)(^[A-Za-z0-9][A-Za-z0-9\-]{1,61}[A-Za-z0-9]$)`)

type Project struct {
	gorm.Model

	Name   string
	UserID uint

	User user.User // belongs to user
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
