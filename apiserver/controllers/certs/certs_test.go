package certs_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers/certs"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "certs")
}

var _ = Describe("Certs", func() {
	var (
		db *gorm.DB

		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	formattedTimeForJSON := func(t time.Time) string {
		formattedTime, err := t.MarshalJSON()
		Expect(err).To(BeNil())
		return string(formattedTime)
	}

	Describe("POST /projects/:project_name/domains/:name/cert", func() {
		var (
			err    error
			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			origAesKey string

			u  *user.User
			oc *oauthclient.OauthClient
			t  *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			dm      *domain.Domain

			certificate []byte
			privateKey  []byte
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			u, oc, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			dm = factories.Domain(db, proj, "www.foo-bar-express.com")

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			certificate = []byte(`-----BEGIN CERTIFICATE-----
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

			privateKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEvwIBADANBgkqhkiG9w0BAQEFAASCBKkwggSlAgEAAoIBAQDaBVRAwKXLWZZX
wP2Ww90JK9WA34/GVmRkyxJ25Y7iNz2WEMFUhvKyGgF7ot/9trF7aTHMMtC+GFMd
bJa95Thmm1JSOvajOWNGu02bQUlXVRE6rAlH9DTRyY1jxN0H9M7VqS9pexAnQMpt
quqP0UmbwXW5APnrqPb5V1gt6pz5nWO66PhBgAXAR7w4tQoW3trJgd+YwV2pQfQ8
itfgMs1q3Xtx9tJbmquE+awgxPRlhaGW539vbTiXd6xxCY1cCYlzuOSy4oWevZzr
f9EpfD5sZYQ3VF1cBLTtttLqqnzV2d+rG5O57p3urFn5wCjNReklJaa/G9Onkdsm
oomPJJQNAgMBAAECggEBALS0lhDVnJXfu20g2Q+NaDehzWTz6AdMtAmXB8bknnmB
r9oiKRwWfjKAu5nudhmkw5a2qj/GHp5xKvLIfmkHBKiHNMRTevnvJwoJVVnJ0zA/
ofgvf8HT97OqizaWhV2C26zcfh+/kLP4T9B2SdTKc2hWAW1GEd/yPEKO0te8DkAV
W/2YoHzWJCQdtU7H4HT5vY1+Tovx4YcWTMSG0OXkLR7OVvvP/RhUD59c3jtIFXX2
YUYZT3zWNZyTpkNagHEEthggmQ9ItDvgLEcSh4bsIaRnGJMDok7MfyhJ1Jm2Oc/Z
fETLmm1PYrgCtnE83zmYtlyK5dem/qYp6TKpeOYO8AECgYEA9xI4Jk1T9Gmhvl/G
Mu/OcYb5vZpHtb2EEQLA286vcpVmSfq/vp9Nj16II2ydWinTYwAeX35saFChwoQI
sFgesBY1nwQFAi4WXuwONcOGzHceND+eIjyTtHkpsT/K63YlL3uyFakZwGU3LB2s
xmhXDPkXKzmh2h8/I4pBJxWx3cECgYEA4eZZqTWXqoNa2AK4eh4eRhZJ3e4vi3Nv
Ejo5e6tQvm4IBNp9nTwuwno8KJluesxrBy7T4aC+0pnjZdvOs3z+WHAH521nyiX7
R8QuWjQsmeuV5g0lDR1CoygfqHmVM7Ez/XvCn/S8Dpc7MzHe+cbsEfyLpMGDhX9i
jid9dMzPIU0CgYEAx8rP5Qk7DrYsuUmxeJc7FcrUQWJ1Ap4SIb9cPWNRtRLi+Ifw
bjFcAseqxxqZ08Nm0PPTm90bxO8PH8CtVgysJDCRg9k4Q58JMBErHIbUhpr8rbuU
IJNjzdj8wfyYFvge8drRE3r++/ndN6t3f6n4WuFCvw2HuF70K8UtEnIUtwECgYAg
2jox5IxhDOdaQNMJV3X5pWYqs2gQtMHzeapAdQKyHxhldE0OX+FBATvcf6vUigQK
sGG6D4GQ6TZr6tKdwdDPlcNggcW1XV606i//iFTwMZXENicsSBQX3E72VnA/a0bv
V19Pmez7hjzizh7qXmaYmwzH8iipcoQnvlB9eweohQKBgQCoJh5hS+pYEQ89bx9U
1jGXxMN44X10sZsqcYluBed140TGzKumZzXoJnwZOUr+jPFotTSyo0+gOc51JqVI
A6ao9QSL1ryillYV9Y4001C3jApzmMtBWoMp3NPzwU8nacAOzClJYUcSLkbAIEWV
2s5+jfbvi7T80pndV0UeagRm/A==
-----END RSA PRIVATE KEY-----`)

			origAesKey = common.AesKey
			common.AesKey = "something-something-something-32"
		})

		AfterEach(func() {
			common.AesKey = origAesKey
			s3client.S3 = origS3
		})

		writePartToBody := func(writer *multipart.Writer, partName string, content []byte) {
			part, err := writer.CreateFormFile(partName, fmt.Sprintf("/tmp/rise/%s", partName))
			Expect(err).To(BeNil())

			_, err = part.Write(content)
			Expect(err).To(BeNil())
		}

		doRequestWithMultipart := func(crtName, pKeyName string) {
			s = httptest.NewServer(server.New())

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			writePartToBody(writer, crtName, certificate)
			writePartToBody(writer, pKeyName, privateKey)
			err = writer.Close()
			Expect(err).To(BeNil())

			req, err := http.NewRequest("POST", s.URL+"/projects/foo-bar-express/domains/"+dm.Name+"/cert", body)
			Expect(err).To(BeNil())

			req.Header.Set("Content-Type", writer.FormDataContentType())
			if headers != nil {
				for k, v := range headers {
					for _, h := range v {
						req.Header.Add(k, h)
					}
				}
			}

			res, err = http.DefaultClient.Do(req)
			Expect(err).To(BeNil())
		}

		doRequest := func() {
			doRequestWithMultipart("cert", "key")
		}

		doRequestWithDefaultDomain := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/domains/foo-bar-express."+shared.DefaultDomain+"/cert", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		doRequestWithWrongPart := func() {
			doRequestWithMultipart("aunt-next-door.jpg", "dont-open-this.txt")
		}

		doRequestWithMissingPart := func() {
			s = httptest.NewServer(server.New())

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			writePartToBody(writer, "cert", certificate)
			err = writer.Close()
			Expect(err).To(BeNil())

			req, err := http.NewRequest("POST", s.URL+"/projects/foo-bar-express/domains/www.foo-bar-express.com/cert", body)
			Expect(err).To(BeNil())

			req.Header.Set("Content-Type", writer.FormDataContentType())
			if headers != nil {
				for k, v := range headers {
					for _, h := range v {
						req.Header.Add(k, h)
					}
				}
			}

			res, err = http.DefaultClient.Do(req)
			Expect(err).To(BeNil())
		}

		doRequestWithoutMultipart := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/domains/www.foo-bar-express.com/cert", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		It("return 201 created", func() {
			doRequest()

			Expect(res.StatusCode).To(Equal(http.StatusCreated))

			ct := &cert.Cert{}
			Expect(db.Last(ct).Error).To(BeNil())

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"cert": {
					"id": %d,
					"starts_at": %s,
					"expires_at": %s,
					"common_name": "%s",
					"issuer": "/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com",
					"subject": "/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"
				}
			}`, ct.ID, formattedTimeForJSON(ct.StartsAt), formattedTimeForJSON(ct.ExpiresAt), *ct.CommonName)))

			Expect(ct.CertificatePath).To(Equal("certs/www.foo-bar-express.com/ssl.crt"))
			Expect(ct.PrivateKeyPath).To(Equal("certs/www.foo-bar-express.com/ssl.key"))
			Expect(ct.StartsAt.String()).To(Equal("2016-04-20 08:50:15 +0000 +0000"))
			Expect(ct.ExpiresAt.String()).To(Equal("2017-04-20 08:50:15 +0000 +0000"))
			Expect(*ct.CommonName).To(Equal("*.foo-bar-express.com"))
			Expect(*ct.Issuer).To(Equal("/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"))
			Expect(*ct.Subject).To(Equal("/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"))
		})

		It("uploads certificate and private keys", func() {
			doRequest()
			Expect(fakeS3.UploadCalls.Count()).To(Equal(2))

			uploaded_contents := [][]byte{certificate, privateKey}
			for i, fileName := range []string{"ssl.crt", "ssl.key"} {
				call := fakeS3.UploadCalls.NthCall(i + 1)
				Expect(call).NotTo(BeNil())
				Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
				Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
				Expect(call.Arguments[2]).To(Equal("certs/www.foo-bar-express.com/" + fileName))
				Expect(call.Arguments[4]).To(Equal(""))
				Expect(call.Arguments[5]).To(Equal("private"))
				encryptedCrt, ok := call.SideEffects["uploaded_content"].([]byte)
				Expect(ok).To(BeTrue())
				decryptedCrt, err := aesencrypter.Decrypt(encryptedCrt, []byte(common.AesKey))
				Expect(err).To(BeNil())
				Expect(decryptedCrt).To(Equal(uploaded_contents[i]))
			}
		})

		Context("when given domain does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(dm).Error).To(BeNil())
			})

			It("returns 404 not found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "domain could not be found"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the payload is larger than limit", func() {
			var origMaxCertSite int64

			BeforeEach(func() {
				origMaxCertSite = certs.MaxCertSize
				certs.MaxCertSize = 10
			})

			AfterEach(func() {
				certs.MaxCertSize = origMaxCertSite
			})

			It("returns 400 bad request", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_request",
					"error_description": "request body is too large"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the domain already has a cert", func() {
			BeforeEach(func() {
				ct := &cert.Cert{
					DomainID:        dm.ID,
					CertificatePath: "old/path",
					PrivateKeyPath:  "old/path",
				}

				Expect(db.Create(ct).Error).To(BeNil())
			})

			It("updates the cert", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusCreated))

				ct := &cert.Cert{}
				Expect(db.Last(ct).Error).To(BeNil())

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"cert": {
						"id": %d,
						"starts_at": %s,
						"expires_at": %s,
						"common_name": "%s",
						"issuer": "/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com",
						"subject": "/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"
					}
			}`, ct.ID, formattedTimeForJSON(ct.StartsAt), formattedTimeForJSON(ct.ExpiresAt), *ct.CommonName)))

				Expect(ct.CertificatePath).To(Equal("certs/www.foo-bar-express.com/ssl.crt"))
				Expect(ct.PrivateKeyPath).To(Equal("certs/www.foo-bar-express.com/ssl.key"))
				Expect(ct.StartsAt.String()).To(Equal("2016-04-20 08:50:15 +0000 +0000"))
				Expect(ct.ExpiresAt.String()).To(Equal("2017-04-20 08:50:15 +0000 +0000"))
				Expect(*ct.CommonName).To(Equal("*.foo-bar-express.com"))
				Expect(*ct.Issuer).To(Equal("/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"))
				Expect(*ct.Subject).To(Equal("/C=SG/O=Nitrous.io/L=Singapore/ST=Singapore/CN=*.foo-bar-express.com"))
			})
		})

		Context("when the request is not multipart", func() {
			It("returns 400 bad request", func() {
				doRequestWithoutMultipart()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_request",
					"error_description": "the request should be encoded in multipart/form-data format"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the default domain is given", func() {
			It("returns 403 forbidden", func() {
				doRequestWithDefaultDomain()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusForbidden))
				Expect(b.String()).To(MatchJSON(`{
					"error": "forbidden",
					"error_description": "not allowed to upload certs for default domain"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the request does not contain cert or private key part", func() {
			It("returns 422 with invalid_params", func() {
				doRequestWithMissingPart()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "both cert and key are required"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the request contains an unrecognized field", func() {
			It("returns 422 with invalid_params", func() {
				doRequestWithWrongPart()

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "unrecognized form field"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("invalid ssl cert is provided", func() {
			BeforeEach(func() {
				certificate = []byte("this is not")
				privateKey = []byte("insecure!!")
			})

			It("returns 422 with invalid_params", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "invalid cert or key"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("ssl cert is not matched domain", func() {
			BeforeEach(func() {
				dm.Name = "www.baz-express.com"
				Expect(db.Save(dm).Error).To(BeNil())
			})

			It("returns 422 with invalid_params", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "invalid common name (domain name mismatch)"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the domain does not belongs to the project", func() {
			BeforeEach(func() {
				proj2 := factories.Project(db, nil)

				dm.ProjectID = proj2.ID
				Expect(db.Save(dm).Error).To(BeNil())
			})

			It("returns 404 with not_found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "domain could not be found"
				}`))

				Expect(fakeS3.UploadCalls.Count()).To(Equal(0))
				ct := &deployment.Deployment{}
				Expect(db.Last(ct).Error).To(Equal(gorm.RecordNotFound))
			})
		})
	})

	Describe("GET /projects/:project_name/domains/:domain_name/cert", func() {
		var (
			u  *user.User
			oc *oauthclient.OauthClient
			t  *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			dm      *domain.Domain
			ct      *cert.Cert
		)

		BeforeEach(func() {
			u, oc, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			dm = factories.Domain(db, proj, "www.foo-bar-express.com")

			wildcardDomainName := "*.foo-bar-express.com"
			ct = &cert.Cert{
				DomainID:   dm.ID,
				ExpiresAt:  time.Now().Add(365 * 24 * time.Hour),
				StartsAt:   time.Now().Add(-365 * 24 * time.Hour),
				CommonName: &wildcardDomainName,
			}
			Expect(db.Create(ct).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/foo-bar-express/domains/www.foo-bar-express.com/cert", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		reloadCert := func(certRecord *cert.Cert) *cert.Cert {
			Expect(db.First(certRecord, ct.ID).Error).To(BeNil())
			return certRecord
		}

		It("returns a cert", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			ct = reloadCert(ct)
			Expect(res.StatusCode).To(Equal(200))
			Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
				"cert": {
					"id": %d,
					"starts_at": %s,
					"expires_at": %s,
					"common_name": "%s"
				}
			}`, ct.ID, formattedTimeForJSON(ct.StartsAt), formattedTimeForJSON(ct.ExpiresAt), *ct.CommonName)))
		})

		Context("when the domain does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(ct).Error).To(BeNil())
				Expect(db.Delete(dm).Error).To(BeNil())
			})

			It("returns 404 with not_found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "cert could not be found"
				}`))
			})
		})

		Context("when the domain does not belongs to the project", func() {
			BeforeEach(func() {
				proj2 := factories.Project(db, nil)

				dm.ProjectID = proj2.ID
				Expect(db.Save(dm).Error).To(BeNil())
			})

			It("returns 404 with not_found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "cert could not be found"
				}`))
			})
		})

		Context("when the cert does not exist", func() {
			BeforeEach(func() {
				Expect(db.Delete(ct).Error).To(BeNil())
			})

			It("returns 404 with not_found", func() {
				doRequest()
				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "cert could not be found"
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})
})
