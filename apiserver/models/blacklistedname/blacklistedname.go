package blacklistedname

import "github.com/jinzhu/gorm"

type BlacklistedName struct {
	Name string
}

func IsBlacklisted(db *gorm.DB, name string) (listed bool, err error) {
	var count int
	q := db.Model(&BlacklistedName{}).Where("name = ?", name).Count(&count)
	if q.Error != nil {
		return false, q.Error
	}

	if count > 0 {
		return true, nil
	}

	return false, nil
}
