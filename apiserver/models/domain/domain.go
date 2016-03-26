package domain

import (
	"regexp"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
)

var domainLabelRe = regexp.MustCompile(`\A([A-Za-z0-9]|([A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9]))\z`)

type Domain struct {
	gorm.Model

	ProjectID uint
	Name      string
}

// Sanitizes domain, e.g. Prepends www if an apex domain is given
func (d *Domain) Sanitize() {
	d.Name = strings.TrimSpace(d.Name)
	labels := strings.Split(d.Name, ".")
	if len(labels) == 2 {
		d.Name = "www." + d.Name
	}
}

// Validates Domain, if there are invalid fields, it returns a map of
// <field, errors> and returns nil if valid
func (d *Domain) Validate() map[string]string {
	errors := map[string]string{}

	if d.Name == "" {
		errors["name"] = "is required"
	} else if len(d.Name) < 3 {
		errors["name"] = "is too short (min. 3 characters)"
	} else if len(d.Name) > 255 {
		errors["name"] = "is too long (max. 255 characters)"
	} else {
		if d.Name == common.DefaultDomain || strings.HasSuffix(d.Name, "."+common.DefaultDomain) {
			errors["name"] = "is invalid"
		} else {
			labels := strings.Split(d.Name, ".")
			if len(labels) < 2 {
				errors["name"] = "is invalid"
			} else {
				for _, label := range labels {
					if label == "" || !domainLabelRe.MatchString(label) {
						errors["name"] = "is invalid"
					}
				}
			}
		}
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

// Returns a struct that can be converted to JSON
func (d *Domain) AsJSON() interface{} {
	return struct {
		Name string `json:"name"`
	}{
		d.Name,
	}
}
