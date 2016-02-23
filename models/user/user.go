package user

import (
	"regexp"

	"github.com/jinzhu/gorm"
)

var emailRe = regexp.MustCompile(`\A[^@\s]+@([^@\s]+\.)+[^@\s]+\z`)

type User struct {
	gorm.Model

	Email        string
	Password     string `sql:"-"`
	Name         string
	Organization string
}

// Returns a struct that can be converted to JSON
func (u *User) AsJSON() interface{} {
	return struct {
		Email        string `json:"email"`
		Name         string `json:"name"`
		Organization string `json:"organization"`
	}{
		u.Email,
		u.Name,
		u.Organization,
	}
}

// Validates User, if there are invalid fields, it returns a map of
// <field, errors> and returns nil if valid
func (u *User) Validate() map[string]string {
	errors := map[string]string{}

	if u.Password == "" {
		errors["password"] = "is required"
	} else if len(u.Password) < 6 {
		errors["password"] = "is too short (min. 6 characters)"
	} else if len(u.Password) > 72 {
		errors["password"] = "is too long (max. 72 characters)"
	}

	if u.Email == "" {
		errors["email"] = "is required"
	} else if len(u.Email) < 5 || !emailRe.MatchString(u.Email) {
		errors["email"] = "is invalid"
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}
