package rawbundle

import "github.com/jinzhu/gorm"

type RawBundle struct {
	gorm.Model

	ProjectID    uint
	Checksum     string
	UploadedPath string
}
