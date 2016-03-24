package user

import (
	"errors"
	"regexp"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

var (
	emailRe = regexp.MustCompile(`\A[^@\s]+@([^@\s]+\.)+[^@\s]+\z`)

	ErrEmailTaken = errors.New("email is taken")
)

type User struct {
	gorm.Model

	Email        string
	Password     string `sql:"-"`
	Name         string
	Organization string

	ConfirmationCode string `sql:"default:lpad((floor(random() * 999999) + 1)::text, 6, '0')"`
	ConfirmedAt      *time.Time
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

// Inserts the record into the DB, encrypting the Password field
func (u *User) Insert(db *gorm.DB) error {
	err := db.Raw(`INSERT INTO users (
		email,
		encrypted_password
	) VALUES (
		?,
		crypt(?, gen_salt('bf'))
	) RETURNING *;`, u.Email, u.Password).Scan(u).Error

	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" && e.Constraint == "index_users_on_email" {
		return ErrEmailTaken
	}
	return err
}

// Checks email and password and return user if credentials are valid
func Authenticate(db *gorm.DB, email, password string) (u *User, err error) {
	u = &User{}
	if err = db.Where(
		"email = ? AND encrypted_password = crypt(?, encrypted_password)",
		email, password).First(u).Error; err != nil {
		// don't treat record not found as error
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return u, err
}

// Finds user by email and confirmation code and confirms user if found
func Confirm(db *gorm.DB, email, confirmationCode string) (confirmed bool, err error) {
	q := db.Model(User{}).Where(
		"email = ? AND confirmation_code = ? AND confirmed_at IS NULL", email, confirmationCode,
	).Update("confirmed_at", gorm.Expr("now()"))
	if err = q.Error; err != nil {
		return false, err
	}

	if q.RowsAffected == 0 {
		return false, nil
	}

	return true, nil
}

// Finds user by email
func FindByEmail(db *gorm.DB, email string) (u *User, err error) {
	u = &User{}
	q := db.Where("email = ?", email).First(u)
	if err = q.Error; err != nil {
		// don't treat record not found as error
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return u, nil
}
