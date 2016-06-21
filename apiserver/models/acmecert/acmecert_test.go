package acmecert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "acmecert")
}

var _ = Describe("AcmeCert", func() {
	var (
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("New()", func() {
		It("sets LetsencryptKey and PrivateKey to randomly generated private keys", func() {
			dm := factories.Domain(db, nil)

			c, err := New(dm.ID, "something-something-something-32")
			Expect(err).To(BeNil())

			Expect(c.DomainID).To(Equal(dm.ID))
			Expect(c.LetsencryptKey).NotTo(BeNil())
			Expect(c.PrivateKey).NotTo(BeNil())
		})
	})

	Describe("encryptPrivateKey / decryptPrivateKey", func() {
		It("successfully encrypts and decrypts", func() {
			privKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).To(BeNil())

			aesKey := "something-something-something-32"
			encrypted, err := encryptPrivateKey(privKey, aesKey)
			Expect(err).To(BeNil())

			decrypted, err := decryptPrivateKey(encrypted, aesKey)
			Expect(err).To(BeNil())
			Expect(decrypted).To(Equal(privKey))
		})
	})

	Describe("SaveCert()", func() {
		It("encodes a cert to PEM data, encrypts it, applies base64 encoding, and saves it", func() {
			dm := factories.Domain(db, nil)

			aesKey := "something-something-something-32"
			acmeCert, err := New(dm.ID, aesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())

			block, _ := pem.Decode(certPEM)
			x509Cert, err := x509.ParseCertificate(block.Bytes)
			Expect(err).To(BeNil())

			// Run some sanity checks that we properly parsed the cert.
			Expect(x509Cert.Issuer.Organization).To(ConsistOf("Nitrous.io"))
			Expect(x509Cert.Issuer.CommonName).To(Equal("*.foo-bar-express.com"))

			err = acmeCert.SaveCert(db, x509Cert, aesKey)
			Expect(err).To(BeNil())

			// Reload from db.
			err = db.First(acmeCert, acmeCert.ID).Error
			Expect(err).To(BeNil())

			// Reverse the encoding and encryption of cert.
			decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(acmeCert.Cert))
			cipherText, err := ioutil.ReadAll(decoder)
			Expect(err).To(BeNil())
			decrypted, err := aesencrypter.Decrypt(cipherText, []byte(aesKey))
			Expect(err).To(BeNil())
			pemBlock, _ := pem.Decode(decrypted)
			cert, err := x509.ParseCertificate(pemBlock.Bytes)
			Expect(err).To(BeNil())

			Expect(cert).To(Equal(x509Cert))
		})
	})

	Describe("DecryptedCert()", func() {
		It("returns the x509.Certificate", func() {
			dm := factories.Domain(db, nil)

			aesKey := "something-something-something-32"
			acmeCert, err := New(dm.ID, aesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())

			block, _ := pem.Decode(certPEM)
			x509Cert, err := x509.ParseCertificate(block.Bytes)
			Expect(err).To(BeNil())

			// Run some sanity checks that we properly parsed the cert.
			Expect(x509Cert.Issuer.Organization).To(ConsistOf("Nitrous.io"))
			Expect(x509Cert.Issuer.CommonName).To(Equal("*.foo-bar-express.com"))

			err = acmeCert.SaveCert(db, x509Cert, aesKey)
			Expect(err).To(BeNil())

			// Reload from db.
			err = db.First(acmeCert, acmeCert.ID).Error
			Expect(err).To(BeNil())

			cert, err := acmeCert.DecryptedCert(aesKey)
			Expect(err).To(BeNil())

			Expect(cert).To(Equal(x509Cert))
		})
	})
})

var certPEM = []byte(`-----BEGIN CERTIFICATE-----
MIID/zCCAuegAwIBAgIJAKLhY+6EFezNMA0GCSqGSIb3DQEBCwUAMIGVMQswCQYD
VQQGEwJTRzESMBAGA1UECAwJU2luZ2Fwb3JlMRIwEAYDVQQHDAlTaW5nYXBvcmUx
EzARBgNVBAoMCk5pdHJvdXMuaW8xHjAcBgNVBAMMFSouZm9vLWJhci1leHByZXNz
LmNvbTEpMCcGCSqGSIb3DQEJARYaZm9vLWJhci1leHByZXNzQG5pdHJvdXMuaW8w
HhcNMTYwNDIwMDg1MDE1WhcNMTcwNDIwMDg1MDE1WjCBlTELMAkGA1UEBhMCU0cx
EjAQBgNVBAgMCVNpbmdhcG9yZTESMBAGA1UEBwwJU2luZ2Fwb3JlMRMwEQYDVQQK
DApOaXRyb3VzLmlvMR4wHAYDVQQDDBUqLmZvby1iYXItZXhwcmVzcy5jb20xKTAn
BgkqhkiG9w0BCQEWGmZvby1iYXItZXhwcmVzc0BuaXRyb3VzLmlvMIIBIjANBgkq
hkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2gVUQMCly1mWV8D9lsPdCSvVgN+PxlZk
ZMsSduWO4jc9lhDBVIbyshoBe6Lf/baxe2kxzDLQvhhTHWyWveU4ZptSUjr2ozlj
RrtNm0FJV1UROqwJR/Q00cmNY8TdB/TO1akvaXsQJ0DKbarqj9FJm8F1uQD566j2
+VdYLeqc+Z1juuj4QYAFwEe8OLUKFt7ayYHfmMFdqUH0PIrX4DLNat17cfbSW5qr
hPmsIMT0ZYWhlud/b204l3escQmNXAmJc7jksuKFnr2c63/RKXw+bGWEN1RdXAS0
7bbS6qp81dnfqxuTue6d7qxZ+cAozUXpJSWmvxvTp5HbJqKJjySUDQIDAQABo1Aw
TjAdBgNVHQ4EFgQUgcRKbfUxwykKmL2RWy6j+6nck1IwHwYDVR0jBBgwFoAUgcRK
bfUxwykKmL2RWy6j+6nck1IwDAYDVR0TBAUwAwEB/zANBgkqhkiG9w0BAQsFAAOC
AQEAiFticlXkTs4lwFdGjdwFYO5bKYcJx5Dj8onktPw6FvpIvpmI3iDja9wlBDCo
GCVTqJjZl9hcT2dne75cA80UcejUfmP42nZtN0+p5ntF2or8vhYs4/jmpWPfHikb
X+QyngquLVUSKH3W/1NbblsL4PtYGpVX9vluAzZlZvz8s/WJagcYEXfekPU5y9oQ
3GFJQhiuYgHrqqiUvY8VI4xq/jddDcn8tKaCTHSoTVzy7UHDAF4JA8EsGrllZPyN
x6bN9vuFFH/ERkYYBJf38RFiOdiQhY/yvVbplHmtMcnywqDuRJAM6brzGIVr6yy4
HFmuSS8xVtPt1xhOwzUAygEWhQ==
-----END CERTIFICATE-----`)
