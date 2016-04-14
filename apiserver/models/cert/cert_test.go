package cert_test

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cert")
}

var _ = Describe("Cert", func() {
	var (
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("Upsert()", func() {
		var (
			ct *cert.Cert
			d  *domain.Domain
		)

		BeforeEach(func() {
			d = factories.Domain(db, nil)
			commonName := "*.foo-bar-express.com"
			ct = &cert.Cert{
				DomainID:        d.ID,
				CertificatePath: "/foo/bar",
				PrivateKeyPath:  "/baz/qux",
				StartsAt:        time.Now(),
				ExpiresAt:       time.Now(),
				CommonName:      &commonName,
			}
		})

		It("inserts a row to certs table", func() {
			Expect(cert.Upsert(db, ct)).To(BeNil())

			var insertedCert cert.Cert
			Expect(db.Last(&insertedCert).Error).To(BeNil())
			Expect(insertedCert.ID).To(Equal(ct.ID))
			Expect(insertedCert.DomainID).To(Equal(d.ID))
			Expect(insertedCert.CertificatePath).To(Equal("/foo/bar"))
			Expect(insertedCert.PrivateKeyPath).To(Equal("/baz/qux"))
		})

		Context("when the record already exists in the DB", func() {
			BeforeEach(func() {
				Expect(db.Create(ct).Error).To(BeNil())
			})

			It("updates the cert", func() {
				ct.CertificatePath = "/legit/foo/bar"
				ct.PrivateKeyPath = "/legit/baz/qux"

				Expect(cert.Upsert(db, ct)).To(BeNil())

				var updatedCert cert.Cert
				Expect(db.Last(&updatedCert).Error).To(BeNil())
				Expect(updatedCert.ID).To(Equal(ct.ID))
				Expect(updatedCert.DomainID).To(Equal(d.ID))
				Expect(updatedCert.CertificatePath).To(Equal("/legit/foo/bar"))
				Expect(updatedCert.PrivateKeyPath).To(Equal("/legit/baz/qux"))
			})
		})
	})
})
