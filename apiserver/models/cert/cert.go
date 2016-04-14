package cert

import (
	"time"

	"github.com/jinzhu/gorm"
)

type Cert struct {
	gorm.Model

	DomainID uint

	CertificatePath string
	PrivateKeyPath  string

	StartsAt   time.Time
	ExpiresAt  time.Time
	CommonName *string
}

// Returns a struct that can be converted to JSON
func (c *Cert) AsJSON() interface{} {
	return struct {
		ID         uint      `json:"id"`
		StartsAt   time.Time `json:"starts_at"`
		ExpiresAt  time.Time `json:"expires_at"`
		CommonName string    `json:"common_name"`
	}{
		c.ID,
		c.StartsAt,
		c.ExpiresAt,
		*c.CommonName,
	}
}

// Upsert a cert
func Upsert(db *gorm.DB, c *Cert) error {
	return db.Raw(`WITH update_cert AS (
		UPDATE certs
		SET certificate_path = $1, private_key_path = $2, starts_at = $4, expires_at = $5, common_name = $6, updated_at = now()
		WHERE domain_id = $3 RETURNING *
	), insert_cert AS (
		INSERT INTO
		certs (domain_id, certificate_path, private_key_path, starts_at, expires_at, common_name)
		SELECT $3, $1, $2, $4, $5, $6 WHERE NOT EXISTS (SELECT * FROM update_cert) RETURNING *
	) SELECT * FROM update_cert UNION ALL SELECT * FROM insert_cert;
  `, c.CertificatePath, c.PrivateKeyPath, c.DomainID, c.StartsAt, c.ExpiresAt, c.CommonName).Scan(c).Error
}
