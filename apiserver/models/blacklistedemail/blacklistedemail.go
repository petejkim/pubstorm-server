package blacklistedemail

import (
	"errors"
	"strings"

	"github.com/jinzhu/gorm"
)

type BlacklistedEmail struct {
	Email string
}

var ErrInvalidEmail = errors.New("email is not valid")

func IsBlacklisted(db *gorm.DB, email string) (listed bool, err error) {
	splitted := strings.Split(email, "@")
	if len(splitted) != 2 {
		return true, ErrInvalidEmail
	}

	var count int
	if err := db.Model(BlacklistedEmail{}).Where("email = ?", splitted[1]).Count(&count).Error; err != nil {
		return true, err
	}

	if count > 0 {
		return true, nil
	}

	return false, nil
}
