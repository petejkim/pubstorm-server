package main

import (
	"encoding/pem"
	"net/http"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "acmerenewal")
}

var _ = Describe("acmerenewal", func() {
	var (
		err error

		db *gorm.DB
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
	})

	Describe("findExpiringAcmeCerts()", func() {
		var (
			dm1, dm2, dm3       *domain.Domain
			cert1, cert2, cert3 *acmecert.AcmeCert
		)

		BeforeEach(func() {
			u, _ := factories.AuthDuo(db)

			proj1 := factories.Project(db, u)
			dm1 = factories.Domain(db, proj1)
			dm2 = factories.Domain(db, proj1)

			proj2 := factories.Project(db, u)
			dm3 = factories.Domain(db, proj2)

			ct1 := &cert.Cert{
				DomainID:        dm1.ID,
				CertificatePath: "certs/1",
				PrivateKeyPath:  "keys/1",
				StartsAt:        time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresAt:       time.Date(2016, 6, 1, 0, 0, 0, 0, time.UTC),
			}
			Expect(db.Create(ct1).Error).To(BeNil())

			cert1 = &acmecert.AcmeCert{
				DomainID:       dm1.ID,
				Cert:           "cert-1",
				LetsencryptKey: "lets-encrypt-key",
				PrivateKey:     "cert-1-key",
			}
			Expect(db.Create(cert1).Error).To(BeNil())

			ct2 := &cert.Cert{
				DomainID:        dm2.ID,
				CertificatePath: "certs/2",
				PrivateKeyPath:  "keys/2",
				StartsAt:        time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresAt:       time.Date(2016, 7, 1, 0, 0, 0, 0, time.UTC),
			}
			Expect(db.Create(ct2).Error).To(BeNil())

			cert2 = &acmecert.AcmeCert{
				DomainID:       dm2.ID,
				Cert:           "cert-2",
				LetsencryptKey: "lets-encrypt-key",
				PrivateKey:     "cert-2-key",
			}
			Expect(db.Create(cert2).Error).To(BeNil())

			ct3 := &cert.Cert{
				DomainID:        dm3.ID,
				CertificatePath: "certs/3",
				PrivateKeyPath:  "keys/3",
				StartsAt:        time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresAt:       time.Date(2016, 8, 1, 0, 0, 0, 0, time.UTC),
			}
			Expect(db.Create(ct3).Error).To(BeNil())

			cert3 = &acmecert.AcmeCert{
				DomainID:       dm3.ID,
				Cert:           "cert-3",
				LetsencryptKey: "lets-encrypt-key",
				PrivateKey:     "cert-3-key",
			}
			Expect(db.Create(cert3).Error).To(BeNil())
		})

		It("returns ACME certificates that expire before the given deadline", func() {
			certs, err := findExpiringAcmeCerts(db, time.Date(2016, 6, 1, 0, 0, 0, 0, time.UTC))
			Expect(err).To(BeNil())

			Expect(certs).To(HaveLen(1))

			Expect(certs[0].Cert).To(Equal(cert1.Cert))

			certs, err = findExpiringAcmeCerts(db, time.Date(2016, 7, 1, 0, 0, 0, 0, time.UTC))
			Expect(err).To(BeNil())

			Expect(certs).To(HaveLen(2))

			domainIDs := []uint{}
			for _, ct := range certs {
				domainIDs = append(domainIDs, ct.DomainID)
			}
			Expect(domainIDs).To(ConsistOf(dm1.ID, dm2.ID))

			certs, err = findExpiringAcmeCerts(db, time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC))
			Expect(err).To(BeNil())

			Expect(certs).To(HaveLen(3))

			domainIDs = []uint{}
			for _, ct := range certs {
				domainIDs = append(domainIDs, ct.DomainID)
			}
			Expect(domainIDs).To(ConsistOf(dm1.ID, dm2.ID, dm3.ID))
		})
	})

	Describe("renew()", func() {
		var (
			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			mq                    *amqp.Connection
			invalidationQueueName string

			acmeServer *ghttp.Server

			renewCertStatus int
			renewCertBody   string
			renewCertHeader http.Header

			currentCertPEM *pem.Block
			renewedCertPEM *pem.Block

			origAesKey  string
			origAcmeURL string

			dm       *domain.Domain
			acmeCert *acmecert.AcmeCert
			ct       *cert.Cert
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			invalidationQueueName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)

			// Decode PEM encoded certs so that we can return them from the mock
			// ACME server in ASN.1 DER format.
			currentCertPEM, _ = pem.Decode(currentCert)
			Expect(currentCertPEM).NotTo(BeNil())
			renewedCertPEM, _ = pem.Decode(renewedCert)
			Expect(renewedCertPEM).NotTo(BeNil())

			renewCertStatus = http.StatusOK
			renewCertBody = string(renewedCertPEM.Bytes)
			renewCertHeader = http.Header{}

			// See https://tools.ietf.org/html/draft-ietf-acme-acme-02 for how
			// an ACME server is supposed to work.
			acmeServer = ghttp.NewServer()
			acmeServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/"),
					ghttp.RespondWith(http.StatusOK, `{
						"new-authz": "`+acmeServer.URL()+`/new-authz",
						"new-cert": "`+acmeServer.URL()+`/new-cert",
						"new-reg": "`+acmeServer.URL()+`/new-reg",
						"revoke-cert": "`+acmeServer.URL()+`/revoke-cert"
					}`, http.Header{"Replay-Nonce": {"nonce-1"}}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/terms"),
					ghttp.RespondWith(http.StatusOK, "ToS PDF file"),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/renew-cert/cert-1"),
					ghttp.RespondWithPtr(&renewCertStatus, &renewCertBody, renewCertHeader),
				),
			)

			origAesKey = common.AesKey
			common.AesKey = "something-something-something-32"

			origAcmeURL = common.AcmeURL
			common.AcmeURL = acmeServer.URL()

			u, _ := factories.AuthDuo(db)
			proj := factories.Project(db, u)
			dm = factories.Domain(db, proj)

			ct = &cert.Cert{
				DomainID:        dm.ID,
				CertificatePath: "certs/1",
				PrivateKeyPath:  "keys/1",
				StartsAt:        time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresAt:       time.Date(2016, 6, 1, 0, 0, 0, 0, time.UTC),
			}
			Expect(db.Create(ct).Error).To(BeNil())

			acmeCert, err = acmecert.New(dm.ID, common.AesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())
			bundledPEM := append(currentCert, issuerCert...)
			err := acmeCert.SaveCert(db, bundledPEM, common.AesKey)
			Expect(err).To(BeNil())
			acmeCert.CertURI = acmeServer.URL() + `/renew-cert/cert-1`
			err = db.Save(acmeCert).Error
			Expect(err).To(BeNil())

			// Reload from DB for reflect.DeepEqual() comparisons to work.
			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			s3client.S3 = origS3
			acmeServer.Close()
			common.AesKey = origAesKey
			common.AcmeURL = origAcmeURL
		})

		It("updates the ACME cert in the DB with the new cert", func() {
			origCert := acmeCert.Cert

			err := renew(db, acmeCert)
			Expect(err).To(BeNil())

			acmeCert2 := &acmecert.AcmeCert{}
			err = db.Where("domain_id = ?", dm.ID).First(acmeCert2).Error
			Expect(err).To(BeNil())

			Expect(acmeCert2.Cert).NotTo(Equal(origCert))

			certChain, err := acmeCert2.DecryptedCerts(common.AesKey)
			Expect(err).To(BeNil())
			x509Cert := certChain[0]
			Expect(x509Cert.Raw).To(Equal(renewedCertPEM.Bytes))

			// These should be unchanged.
			Expect(acmeCert2.DomainID).To(Equal(dm.ID))
			Expect(acmeCert2.LetsencryptKey).To(Equal(acmeCert.LetsencryptKey))
			Expect(acmeCert2.PrivateKey).To(Equal(acmeCert.PrivateKey))
		})

		It("updates the issue and expiry times of the associated cert", func() {
			err := renew(db, acmeCert)
			Expect(err).To(BeNil())

			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			certChain, err := acmeCert.DecryptedCerts(common.AesKey)
			Expect(err).To(BeNil())
			x509Cert := certChain[0]

			ct := &cert.Cert{}
			err = db.Where("domain_id = ?", dm.ID).First(ct).Error
			Expect(err).To(BeNil())

			Expect(ct.StartsAt.UTC()).To(Equal(x509Cert.NotBefore.UTC()))
			Expect(ct.ExpiresAt.UTC()).To(Equal(x509Cert.NotAfter.UTC()))
		})

		It("uploads the new certificate to S3", func() {
			err := renew(db, acmeCert)
			Expect(err).To(BeNil())

			Expect(fakeS3.UploadCalls.Count()).To(Equal(1))

			call := fakeS3.UploadCalls.NthCall(1)
			Expect(call).NotTo(BeNil())
			Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(call.Arguments[2]).To(Equal("certs/" + dm.Name + "/ssl.crt"))
			Expect(call.Arguments[4]).To(Equal(""))
			Expect(call.Arguments[5]).To(Equal("private"))
			encryptedCrt, ok := call.SideEffects["uploaded_content"].([]byte)
			Expect(ok).To(BeTrue())
			decryptedCrt, err := aesencrypter.Decrypt(encryptedCrt, []byte(common.AesKey))
			Expect(err).To(BeNil())
			bundledPEM := append(renewedCert, issuerCert...)
			Expect(decryptedCrt).To(Equal(bundledPEM))
		})

		It("publishes invalidation message for the domain", func() {
			err := renew(db, acmeCert)
			Expect(err).To(BeNil())

			d := testhelper.ConsumeQueue(mq, invalidationQueueName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{"domains": ["` + dm.Name + `"]}`))
		})

		Context("when Let's Encrypt does not return a certificate", func() {
			BeforeEach(func() {
				renewCertBody = ``
				// ACME spec requires a Retry-After header if certificate is unavailable.
				// See https://tools.ietf.org/html/draft-ietf-acme-acme-02#section-6.5.
				renewCertHeader.Add("Retry-After", "120")
			})

			It("does not update nor upload the cert", func() {
				err := renew(db, acmeCert)
				Expect(err).To(BeNil())

				acmeCert2 := &acmecert.AcmeCert{}
				err = db.Where("domain_id = ?", dm.ID).First(acmeCert2).Error
				Expect(err).To(BeNil())
				Expect(acmeCert2).To(Equal(acmeCert))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
			})
		})

		Context("when Let's Encrypt returns an identical certificate", func() {
			BeforeEach(func() {
				renewCertBody = string(currentCertPEM.Bytes)
			})

			It("does not update nor upload the cert", func() {
				err := renew(db, acmeCert)
				Expect(err).To(BeNil())

				acmeCert2 := &acmecert.AcmeCert{}
				err = db.Where("domain_id = ?", dm.ID).First(acmeCert2).Error
				Expect(err).To(BeNil())
				Expect(acmeCert2).To(Equal(acmeCert))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
			})
		})
	})
})

var currentCert = []byte(`-----BEGIN CERTIFICATE-----
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

var renewedCert = []byte(`-----BEGIN CERTIFICATE-----
MIIE+TCCA+GgAwIBAgITAPq9qBUK0iqy49/O7F9XL4t1QzANBgkqhkiG9w0BAQsF
ADAiMSAwHgYDVQQDDBdGYWtlIExFIEludGVybWVkaWF0ZSBYMTAeFw0xNjA2Mjcx
MzE5MDBaFw0xNjA5MjUxMzE5MDBaMCUxIzAhBgNVBAMTGnB1YnN0b3JtLWRldi5j
b2RlZnJvbnQubmV0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvymD
VLuRHcfqSdlxsRxyUbXXsxXTx+7dIIdeeg0exFS6KD3CiS2TAmG+f+/GqvEIv0lu
LMILUtTMk4LD3I+gsdo4WRuxlQAU8PaBCXiuMkfZNesAmTYSHF8chI84dS20GbCs
MlOBWASfff9yoTSzDmH/vJ+jG76A1Gl22vHGdCow77DcDkdIjatd91GDzJFdGOJP
ne1wVlkCffG4M3cambZm+oL8x/xkD5JrD4oimpTLNh3PmdWi40RzfSatMKmPwiLp
F2Bz0A8CQFCLS6ET8kmt9zGzLvLMysdAYwm/GoRqHUOzIrYpUpFMUXDbHDchjLPL
PuVe06r1b6EXzOHfkwIDAQABo4ICIzCCAh8wDgYDVR0PAQH/BAQDAgWgMB0GA1Ud
JQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAMBgNVHRMBAf8EAjAAMB0GA1UdDgQW
BBQStHnPIHwPmoMqbWCE5BSQyM72izAfBgNVHSMEGDAWgBTAzANGuVggzFxycPPh
LssgpvVoOjB4BggrBgEFBQcBAQRsMGowMwYIKwYBBQUHMAGGJ2h0dHA6Ly9vY3Nw
LnN0Zy1pbnQteDEubGV0c2VuY3J5cHQub3JnLzAzBggrBgEFBQcwAoYnaHR0cDov
L2NlcnQuc3RnLWludC14MS5sZXRzZW5jcnlwdC5vcmcvMCUGA1UdEQQeMByCGnB1
YnN0b3JtLWRldi5jb2RlZnJvbnQubmV0MIH+BgNVHSAEgfYwgfMwCAYGZ4EMAQIB
MIHmBgsrBgEEAYLfEwEBATCB1jAmBggrBgEFBQcCARYaaHR0cDovL2Nwcy5sZXRz
ZW5jcnlwdC5vcmcwgasGCCsGAQUFBwICMIGeDIGbVGhpcyBDZXJ0aWZpY2F0ZSBt
YXkgb25seSBiZSByZWxpZWQgdXBvbiBieSBSZWx5aW5nIFBhcnRpZXMgYW5kIG9u
bHkgaW4gYWNjb3JkYW5jZSB3aXRoIHRoZSBDZXJ0aWZpY2F0ZSBQb2xpY3kgZm91
bmQgYXQgaHR0cHM6Ly9sZXRzZW5jcnlwdC5vcmcvcmVwb3NpdG9yeS8wDQYJKoZI
hvcNAQELBQADggEBADctT6wgNyoDdl35ZQ+H/29BvZzK2ukQ3KGToMHqg6j17Dcb
oOE9EEoNBoCwZYnFyTzj8kh+zTs81zfVjh28Nspv5yIDd9P+eAARbuuJBefembV4
nq6tJPyvkMa+IFcjP2n8vWxAZ8dQLsTBPrV9cJgSDXb/O1WlUVucmoP/vzG5YucQ
t2LI8FE9aicCBk4WjgVRylQY0FNFHrjes4UEV8G+T9lOv/HNcrhoSVCdQFLM6JMv
EFfRAVGGqD5DrVIg/h6n3kBuYQBhZHfpBJO3OTr6cM5JPoAlDTxCTS0XPiLHwzSp
Lzg/Mt7dX5YanQFB+M8/nU1PCAgFRU52uoEQ60Y=
-----END CERTIFICATE-----
`)

var issuerCert = []byte(`-----BEGIN CERTIFICATE-----
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
