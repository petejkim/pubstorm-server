package user

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

var (
	emailRe = regexp.MustCompile(`\A[^@\s]+@([^@\s]+\.)+[^@\s]+\z`)
)

// Errors returned from this package.
var (
	ErrEmailTaken                  = errors.New("email is taken")
	ErrPasswordResetTokenRequired  = errors.New("password reset token is required")
	ErrPasswordResetTokenIncorrect = errors.New("password reset token incorrect")
)

// User is a database model representing a user account,
type User struct {
	gorm.Model

	Email        string
	Password     string `sql:"-"`
	Name         string
	Organization string

	ConfirmationCode string `sql:"default:lpad((floor(random() * 999999) + 1)::text, 6, '0')"`
	ConfirmedAt      *time.Time

	PasswordResetToken          string
	PasswordResetTokenCreatedAt *time.Time
}

// AsJSON returns a struct that can be converted to JSON
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

// Validate validates User, if there are invalid fields, it returns a map of
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

// Insert saves the record to the DB, encrypting the Password field
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

// SavePassword encrypts and updates the user's password.
func (u *User) SavePassword(db *gorm.DB) error {
	return db.Exec("UPDATE users SET encrypted_password = crypt(?, gen_salt('bf')) WHERE id = ?;", u.Password, u.ID).Error
}

// GeneratePasswordResetToken generates a unique token for the user to be used
// for resetting their password (without requiring them to authenticate via
// their password).
func (u *User) GeneratePasswordResetToken(db *gorm.DB) error {
	// Generate a random string.
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return err
	}

	token := base64.URLEncoding.EncodeToString(b)
	now := time.Now()

	return db.Model(u).Updates(User{
		PasswordResetToken:          token,
		PasswordResetTokenCreatedAt: &now,
	}).Error
}

// ResetPassword attempts to set a user's password to the new password. The
// resetToken must not be empty, and must match the password reset token. If
// successful, the password reset token will be cleared (so that the token
// cannot be re-used).
func (u *User) ResetPassword(db *gorm.DB, newPassword, resetToken string) error {
	// Explicitly disallow an empty token (since a user will not have a password
	// reset token by default).
	if resetToken == "" {
		return ErrPasswordResetTokenRequired
	}

	// TODO We should check password_reset_token_created_at.

	q := db.Raw(`UPDATE users
        SET
            encrypted_password = crypt(?, gen_salt('bf')),
            password_reset_token = NULL,
            password_reset_token_created_at = NULL
        WHERE id = ? AND password_reset_token = ?
        RETURNING *;`, newPassword, u.ID, resetToken).Scan(u)

	if err := q.Error; err != nil {
		if err == gorm.RecordNotFound {
			return ErrPasswordResetTokenIncorrect
		}
		return err
	}

	return nil
}

// Authenticate checks email and password and return user if credentials are valid
func Authenticate(db *gorm.DB, email, password string) (*User, error) {
	u := &User{}
	if err := db.Where(
		"email = ? AND encrypted_password = crypt(?, encrypted_password)",
		email, password).First(u).Error; err != nil {
		// don't treat record not found as error
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return u, nil
}

// Confirm finds user by email and confirmation code and confirms user if found
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

// FindByEmail returns the user with the given email
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
