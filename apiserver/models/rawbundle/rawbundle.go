package rawbundle

import "github.com/jinzhu/gorm"

type RawBundle struct {
	gorm.Model

	ProjectID    uint
	Checksum     string
	UploadedPath string
}

// Returns a struct that can be converted to JSON
func (b *RawBundle) AsJSON() interface{} {
	return struct {
		ID           uint   `json:"id"`
		Checksum     string `json:"checksum"`
		UploadedPath string `json:"uploaded_path"`
	}{
		b.ID,
		b.Checksum,
		b.UploadedPath,
	}
}
