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
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
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

	Describe("IsValid()", func() {
		It("returns true if domain ID, certificate, and private keys are non-zero", func() {
			dm := factories.Domain(db, nil)
			c := AcmeCert{
				DomainID:       dm.ID,
				Cert:           "super-secure-cert",
				LetsencryptKey: "lets-encrypt-key",
				PrivateKey:     "cert-key",
			}
			Expect(c.IsValid()).To(BeTrue())
		})

		It("returns false if any required fields are zero value", func() {
			dm := factories.Domain(db, nil)
			c := AcmeCert{
				DomainID:       dm.ID,
				Cert:           "super-secure-cert",
				LetsencryptKey: "lets-encrypt-key",
				PrivateKey:     "cert-key",
			}
			Expect(c.IsValid()).To(BeTrue())

			c1 := c
			c1.DomainID = 0
			Expect(c1.IsValid()).To(BeFalse())

			c2 := c
			c2.Cert = ""
			Expect(c2.IsValid()).To(BeFalse())

			c3 := c
			c3.LetsencryptKey = ""
			Expect(c3.IsValid()).To(BeFalse())

			c4 := c
			c4.PrivateKey = ""
			Expect(c4.IsValid()).To(BeFalse())
		})
	})

	Describe("SaveCert()", func() {
		It("encrypts a PEM-encoded cert, applies base64 encoding, and saves it", func() {
			dm := factories.Domain(db, nil)

			aesKey := "something-something-something-32"
			acmeCert, err := New(dm.ID, aesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())

			err = acmeCert.SaveCert(db, certPEM, aesKey)
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

			Expect(decrypted).To(Equal(certPEM))
		})

		Context("when saving a cert bundle", func() {
			It("encrypts, applies base64 encoding, and saves it", func() {
				bundledPEM := append(certPEM, issuerCertPEM...)

				dm := factories.Domain(db, nil)

				aesKey := "something-something-something-32"
				acmeCert, err := New(dm.ID, aesKey)
				Expect(err).To(BeNil())
				Expect(db.Create(acmeCert).Error).To(BeNil())

				err = acmeCert.SaveCert(db, bundledPEM, aesKey)
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

				Expect(decrypted).To(Equal(bundledPEM))
			})
		})
	})

	Describe("DecryptedCerts()", func() {
		var (
			acmeCert *AcmeCert
			dm       *domain.Domain
			aesKey   = "something-something-something-32"
		)

		BeforeEach(func() {
			dm = factories.Domain(db, nil)
		})

		Context("when .Cert is a single certificate", func() {
			BeforeEach(func() {
				var err error
				acmeCert, err = New(dm.ID, aesKey)
				Expect(err).To(BeNil())
				Expect(db.Create(acmeCert).Error).To(BeNil())

				err = acmeCert.SaveCert(db, certPEM, aesKey)
				Expect(err).To(BeNil())
			})

			It("returns the x509.Certificate", func() {
				// Reload from db.
				err = db.First(acmeCert, acmeCert.ID).Error
				Expect(err).To(BeNil())

				certChain, err := acmeCert.DecryptedCerts(aesKey)
				Expect(err).To(BeNil())

				Expect(certChain).To(HaveLen(1))

				block, _ := pem.Decode(certPEM)
				x509Cert, err := x509.ParseCertificate(block.Bytes)
				Expect(err).To(BeNil())

				// Run some sanity checks that we properly parsed the cert.
				Expect(x509Cert.Issuer.Organization).To(ConsistOf("Nitrous.io"))
				Expect(x509Cert.Issuer.CommonName).To(Equal("*.foo-bar-express.com"))

				Expect(certChain[0]).To(Equal(x509Cert))
			})
		})

		Context("when .Cert is a certificate bundle", func() {
			BeforeEach(func() {
				var err error
				acmeCert, err = New(dm.ID, aesKey)
				Expect(err).To(BeNil())
				Expect(db.Create(acmeCert).Error).To(BeNil())

				bundledPEM := append(certPEM, issuerCertPEM...)

				err = acmeCert.SaveCert(db, bundledPEM, aesKey)
				Expect(err).To(BeNil())
			})

			It("returns the x509.Certificates", func() {
				// Reload from db.
				err = db.First(acmeCert, acmeCert.ID).Error
				Expect(err).To(BeNil())

				certChain, err := acmeCert.DecryptedCerts(aesKey)
				Expect(err).To(BeNil())

				Expect(certChain).To(HaveLen(2))

				certBlock, _ := pem.Decode(certPEM)
				cert, err := x509.ParseCertificate(certBlock.Bytes)
				Expect(err).To(BeNil())

				// Run some sanity checks that we properly parsed the cert.
				Expect(cert.Issuer.Organization).To(ConsistOf("Nitrous.io"))
				Expect(cert.Issuer.CommonName).To(Equal("*.foo-bar-express.com"))

				Expect(certChain[0]).To(Equal(cert))

				issuerCertBlock, _ := pem.Decode(issuerCertPEM)
				issuerCert, err := x509.ParseCertificate(issuerCertBlock.Bytes)
				Expect(err).To(BeNil())

				// Run some sanity checks that we properly parsed the cert.
				Expect(issuerCert.Issuer.Organization).To(ConsistOf("Digital Signature Trust Co."))
				Expect(issuerCert.Issuer.CommonName).To(Equal("DST Root CA X3"))

				Expect(certChain[1]).To(Equal(issuerCert))
			})
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
-----END CERTIFICATE-----
`)

var issuerCertPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIEkjCCA3qgAwIBAgIQCgFBQgAAAVOFc2oLheynCDANBgkqhkiG9w0BAQsFADA/
MSQwIgYDVQQKExtEaWdpdGFsIFNpZ25hdHVyZSBUcnVzdCBDby4xFzAVBgNVBAMT
DkRTVCBSb290IENBIFgzMB4XDTE2MDMxNzE2NDA0NloXDTIxMDMxNzE2NDA0Nlow
SjELMAkGA1UEBhMCVVMxFjAUBgNVBAoTDUxldCdzIEVuY3J5cHQxIzAhBgNVBAMT
GkxldCdzIEVuY3J5cHQgQXV0aG9yaXR5IFgzMIIBIjANBgkqhkiG9w0BAQEFAAOC
AQ8AMIIBCgKCAQEAnNMM8FrlLke3cl03g7NoYzDq1zUmGSXhvb418XCSL7e4S0EF
q6meNQhY7LEqxGiHC6PjdeTm86dicbp5gWAf15Gan/PQeGdxyGkOlZHP/uaZ6WA8
SMx+yk13EiSdRxta67nsHjcAHJyse6cF6s5K671B5TaYucv9bTyWaN8jKkKQDIZ0
Z8h/pZq4UmEUEz9l6YKHy9v6Dlb2honzhT+Xhq+w3Brvaw2VFn3EK6BlspkENnWA
a6xK8xuQSXgvopZPKiAlKQTGdMDQMc2PMTiVFrqoM7hD8bEfwzB/onkxEz0tNvjj
/PIzark5McWvxI0NHWQWM6r6hCm21AvA2H3DkwIDAQABo4IBfTCCAXkwEgYDVR0T
AQH/BAgwBgEB/wIBADAOBgNVHQ8BAf8EBAMCAYYwfwYIKwYBBQUHAQEEczBxMDIG
CCsGAQUFBzABhiZodHRwOi8vaXNyZy50cnVzdGlkLm9jc3AuaWRlbnRydXN0LmNv
bTA7BggrBgEFBQcwAoYvaHR0cDovL2FwcHMuaWRlbnRydXN0LmNvbS9yb290cy9k
c3Ryb290Y2F4My5wN2MwHwYDVR0jBBgwFoAUxKexpHsscfrb4UuQdf/EFWCFiRAw
VAYDVR0gBE0wSzAIBgZngQwBAgEwPwYLKwYBBAGC3xMBAQEwMDAuBggrBgEFBQcC
ARYiaHR0cDovL2Nwcy5yb290LXgxLmxldHNlbmNyeXB0Lm9yZzA8BgNVHR8ENTAz
MDGgL6AthitodHRwOi8vY3JsLmlkZW50cnVzdC5jb20vRFNUUk9PVENBWDNDUkwu
Y3JsMB0GA1UdDgQWBBSoSmpjBH3duubRObemRWXv86jsoTANBgkqhkiG9w0BAQsF
AAOCAQEA3TPXEfNjWDjdGBX7CVW+dla5cEilaUcne8IkCJLxWh9KEik3JHRRHGJo
uM2VcGfl96S8TihRzZvoroed6ti6WqEBmtzw3Wodatg+VyOeph4EYpr/1wXKtx8/
wApIvJSwtmVi4MFU5aMqrSDE6ea73Mj2tcMyo5jMd6jmeWUHK8so/joWUoHOUgwu
X4Po1QYz+3dszkDqMp4fklxBwXRsW10KXzPMTZ+sOPAveyxindmjkW8lGy+QsRlG
PfZ+G6Z6h7mjem0Y+iWlkYcV4PIWL1iwBi8saCbGS5jN2p8M+X+Q7UNKEkROb3N6
KOqkqm57TH2H3eDJAkSnh6/DNFu0Qg==
-----END CERTIFICATE-----
`)
