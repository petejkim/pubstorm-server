package factories

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/template"

	. "github.com/onsi/gomega"
)

var templateN = 0

func Template(db *gorm.DB, rank int, name ...string) *template.Template {
	var tName string
	if len(name) > 0 {
		tName = name[0]
	} else {
		templateN++
		tName = fmt.Sprintf("template%04d", projectN)
	}

	tmpl := &template.Template{
		Name:            tName,
		Rank:            rank,
		DownloadURL:     fmt.Sprintf("/templates/template-%s.tar.gz", tName),
		PreviewURL:      fmt.Sprintf("https://example.com/preview/template-%s", tName),
		PreviewImageURL: fmt.Sprintf("https://example.com/preview/template-%s.png", tName),
	}

	err := db.Create(tmpl).Error
	Expect(err).To(BeNil())

	return tmpl
}
