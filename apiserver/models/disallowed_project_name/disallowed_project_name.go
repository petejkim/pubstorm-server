package disallowed_project_name

import "github.com/jinzhu/gorm"

type DisallowedProjectName struct {
	Name string
}

func IsBlacklisted(db *gorm.DB, name string) (listed bool, err error) {
	var count int
	q := db.Model(&DisallowedProjectName{}).Where("name = ?", name).Count(&count)
	if q.Error != nil {
		return false, q.Error
	}

	if count > 0 {
		return true, nil
	}

	return false, nil
}
