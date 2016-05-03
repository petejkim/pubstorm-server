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

	StartsAt  time.Time
	ExpiresAt time.Time

	CommonName *string
	Issuer     *string
	Subject    *string
}

// Returns a struct that can be converted to JSON
func (c *Cert) AsJSON() interface{} {
	j := struct {
		ID         uint      `json:"id"`
		StartsAt   time.Time `json:"starts_at"`
		ExpiresAt  time.Time `json:"expires_at"`
		CommonName string    `json:"common_name,omitempty"`
		Issuer     string    `json:"issuer,omitempty"`
		Subject    string    `json:"subject,omitempty"`
	}{
		ID:        c.ID,
		StartsAt:  c.StartsAt,
		ExpiresAt: c.ExpiresAt,
	}

	if c.CommonName != nil {
		j.CommonName = *c.CommonName
	}

	if c.Issuer != nil {
		j.Issuer = *c.Issuer
	}

	if c.Subject != nil {
		j.Subject = *c.Subject
	}

	return j
}

// Upsert a cert
func Upsert(db *gorm.DB, c *Cert) error {
	return db.Raw(`WITH update_cert AS (
		UPDATE certs
		SET certificate_path=$2, private_key_path=$3, starts_at=$4, expires_at=$5, common_name=$6, issuer=$7, subject=$8, updated_at = now()
		WHERE domain_id=$1 RETURNING *
	), insert_cert AS (
		INSERT INTO
		certs (domain_id, certificate_path, private_key_path, starts_at, expires_at, common_name, issuer, subject)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8 WHERE NOT EXISTS (SELECT * FROM update_cert) RETURNING *
	) SELECT * FROM update_cert UNION ALL SELECT * FROM insert_cert;
  `,
		c.DomainID,        // $1
		c.CertificatePath, // $2
		c.PrivateKeyPath,  // $3
		c.StartsAt,        // $4
		c.ExpiresAt,       // $5
		c.CommonName,      // $6
		c.Issuer,          // $7
		c.Subject,         // $8
	).Scan(c).Error
}
