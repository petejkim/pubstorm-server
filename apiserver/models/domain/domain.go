package domain

import (
	"regexp"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/shared"
	"golang.org/x/net/publicsuffix"
)

var domainLabelRe = regexp.MustCompile(`\A([a-z0-9]|([a-z0-9][a-z0-9\-]*[a-z0-9]))\z`)

type Domain struct {
	gorm.Model

	ProjectID uint
	Name      string
}

// JSON specifies which fields of a domain will be marshaled to JSON.
type JSON struct {
	Name  string `json:"name"`
	HTTPS *bool  `json:"https,omitempty"`
}

// Sanitizes domain, e.g. Prepends www if an apex domain is given
// i.e. Prepends www to "abc.com", "abc.au", "abc.com.au", "abc.co.au"
func (d *Domain) Sanitize() error {
	d.Name = strings.TrimSpace(d.Name)
	apexDomain, err := publicsuffix.EffectiveTLDPlusOne(d.Name)
	if err != nil {
		return err
	}

	if d.Name == apexDomain {
		d.Name = "www." + d.Name
	}

	return nil
}

// Validates Domain, if there are invalid fields, it returns a map of
// <field, errors> and returns nil if valid
func (d *Domain) Validate() map[string]string {
	errors := map[string]string{}

	if len(d.Name) < 3 {
		errors["name"] = "is too short (min. 3 characters)"
	} else if len(d.Name) > 255 {
		errors["name"] = "is too long (max. 255 characters)"
	} else {
		if d.Name == shared.DefaultDomain || strings.HasSuffix(d.Name, "."+shared.DefaultDomain) {
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
	return JSON{
		Name: d.Name,
	}
}

// Domain with protocol
type DomainWithProtocol struct {
	Domain
	HTTPS bool `sql:"column:https"`
}

// Returns table name
func (dp *DomainWithProtocol) TableName() string {
	return "domains"
}

// Returns a struct that can be converted to JSON
func (dp *DomainWithProtocol) AsJSON() interface{} {
	return JSON{
		Name:  dp.Name,
		HTTPS: &dp.HTTPS,
	}
}
