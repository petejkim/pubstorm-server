package template

import (
	"github.com/jinzhu/gorm"
)

type Template struct {
	gorm.Model

	Name string
	Rank int

	// DownloadURL is the path to this template on S3.
	// Templates should be gzipped tarballs, for now.
	DownloadURL     string `sql:"column:download_url"`
	PreviewURL      string `sql:"column:preview_url"`
	PreviewImageURL string `sql:"column:preview_image_url"`
}

func (t *Template) AsJSON() interface{} {
	return struct {
		ID              uint   `json:"id"`
		Name            string `json:"name"`
		Rank            int    `json:"rank"`
		DownloadURL     string `json:"download_url"`
		PreviewURL      string `json:"preview_url"`
		PreviewImageURL string `json:"preview_image_url"`
	}{
		ID:              t.ID,
		Name:            t.Name,
		Rank:            t.Rank,
		DownloadURL:     t.DownloadURL,
		PreviewURL:      t.PreviewURL,
		PreviewImageURL: t.PreviewImageURL,
	}
}
