package collab

import "github.com/jinzhu/gorm"

type Collab struct {
	gorm.Model

	UserID    uint
	ProjectID uint
}
