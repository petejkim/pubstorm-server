package factories

import (
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"

	. "github.com/onsi/gomega"
)

func Cert(db *gorm.DB, d *domain.Domain) (c *cert.Cert) {
	if d == nil {
		d = Domain(db, nil)
	}

	c = &cert.Cert{
		DomainID:        d.ID,
		CertificatePath: "path/to/crt",
		PrivateKeyPath:  "path/to/key",
	}

	err := db.Create(c).Error
	Expect(err).To(BeNil())

	return c
}
