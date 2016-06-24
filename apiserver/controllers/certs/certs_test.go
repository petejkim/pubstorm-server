package certs_test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
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
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
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
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/pkg/tracker"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/streadway/amqp"
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

		fakeTracker *fake.Tracker
		origTracker tracker.Trackable
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())

		testhelper.TruncateTables(db.DB())

		origTracker = common.Tracker
		fakeTracker = &fake.Tracker{}
		common.Tracker = fakeTracker
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()

		common.Tracker = origTracker
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

			mq                    *amqp.Connection
			invalidationQueueName string

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

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			invalidationQueueName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)

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
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/foo-bar-express/domains/"+proj.DefaultDomainName()+"/cert", nil, headers, nil)
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

		It("uploads certificate and private key", func() {
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

		It("publishes invalidation message for the domain", func() {
			doRequest()

			d := testhelper.ConsumeQueue(mq, invalidationQueueName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{"domains": ["www.foo-bar-express.com"]}`))
		})

		It("tracks an 'Uploaded SSL Certificate' event", func() {
			doRequest()

			trackCall := fakeTracker.TrackCalls.NthCall(1)
			Expect(trackCall).NotTo(BeNil())
			Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
			Expect(trackCall.Arguments[1]).To(Equal("Uploaded SSL Certificate"))
			Expect(trackCall.Arguments[2]).To(Equal(""))

			t := trackCall.Arguments[3]
			props, ok := t.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(props["projectName"]).To(Equal("foo-bar-express"))
			Expect(props["domain"]).To(Equal("www.foo-bar-express.com"))
			Expect(props["certSize"]).To(Equal(len(certificate)))
			Expect(props["certKeySize"]).To(Equal(len(privateKey)))

			ct := &cert.Cert{}
			Expect(db.Last(ct).Error).To(BeNil())
			Expect(props["certId"]).To(Equal(ct.ID))
			Expect(props["certIssuer"]).To(Equal(ct.Issuer))
			Expect(props["certExpiresAt"]).To(Equal(ct.ExpiresAt))

			Expect(trackCall.Arguments[4]).To(BeNil())
			Expect(trackCall.ReturnValues[0]).To(BeNil())
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

			It("publishes invalidation message for the domain", func() {
				doRequest()

				d := testhelper.ConsumeQueue(mq, invalidationQueueName)
				Expect(d).NotTo(BeNil())
				Expect(d.Body).To(MatchJSON(`{"domains": ["www.foo-bar-express.com"]}`))
			})

			It("tracks an 'Uploaded SSL Certificate' event", func() {
				doRequest()

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Uploaded SSL Certificate"))
				Expect(trackCall.Arguments[2]).To(Equal(""))

				t := trackCall.Arguments[3]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal("foo-bar-express"))
				Expect(props["domain"]).To(Equal("www.foo-bar-express.com"))
				Expect(props["certSize"]).To(Equal(len(certificate)))
				Expect(props["certKeySize"]).To(Equal(len(privateKey)))

				ct := &cert.Cert{}
				Expect(db.Last(ct).Error).To(BeNil())
				Expect(props["certId"]).To(Equal(ct.ID))
				Expect(props["certIssuer"]).To(Equal(ct.Issuer))
				Expect(props["certExpiresAt"]).To(Equal(ct.ExpiresAt))

				Expect(trackCall.Arguments[4]).To(BeNil())
				Expect(trackCall.ReturnValues[0]).To(BeNil())
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

		Context("when the domain does not belong to the project", func() {
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

	Describe("POST /projects/:project_name/domains/:name/cert/letsencrypt", func() {
		var (
			headers http.Header

			u    *user.User
			t    *oauthtoken.OauthToken
			proj *project.Project
			dm   *domain.Domain

			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			mq                    *amqp.Connection
			invalidationQueueName string

			acmeServer *ghttp.Server

			origAesKey  string
			origAcmeURL string

			letsencryptPEM       *pem.Block
			letsencryptIssuerPEM *pem.Block

			// These values can be changed in tests to test cases other than the
			// "happy path".
			newAuthzStatusCode  int
			newAuthzBody        string
			challengeStatusCode int
			challengeBody       string
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)

			proj = &project.Project{
				Name:   "foo-bar-express",
				UserID: u.ID,
			}
			Expect(db.Create(proj).Error).To(BeNil())

			dm = factories.Domain(db, proj, "www.foo-bar-express.com")

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			invalidationQueueName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)

			// Decode PEM encoded certs so that we can return them from the mock
			// ACME server in ASN.1 DER format.
			letsencryptPEM, _ = pem.Decode(letsencryptCert)
			Expect(letsencryptPEM).NotTo(BeNil())
			letsencryptIssuerPEM, _ = pem.Decode(letsencryptIssuerCert)
			Expect(letsencryptIssuerPEM).NotTo(BeNil())

			acmeServer = ghttp.NewServer()

			newAuthzStatusCode = http.StatusCreated
			newAuthzBody = `{
				"identifier": {
					"type": "dns",
					"value": "www.foo-bar-express.com"
				},
				"status": "pending",
				"expires": "2016-06-28T09:41:07.002634342Z",
				"challenges": [
					{
						"type": "http-01",
						"status": "pending",
						"uri": "` + acmeServer.URL() + `/acme/challenge/abcde/124",
						"token": "secret-token"
					}
				],
				"combinations": [
					[0],
					[1],
					[2]
				]
			}`
			challengeStatusCode = http.StatusAccepted
			challengeBody = `{ "status": "valid" }`

			// See https://tools.ietf.org/html/draft-ietf-acme-acme-02 for how
			// an ACME server is supposed to work.
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
					ghttp.VerifyRequest("POST", "/new-reg"),
					ghttp.VerifyContentType("application/jose+jws"),
					ghttp.RespondWith(http.StatusCreated, `{
						"resource": "new-reg",
						"contact": [
							"mailto:cert-admin@example.com"
						],
						"agreement": "`+acmeServer.URL()+`/terms",
						"authorizations": "",
						"certificates": ""
					}`, http.Header{"Replay-Nonce": {"nonce-2"}}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/new-authz"),
					ghttp.VerifyContentType("application/jose+jws"),
					ghttp.RespondWithPtr(&newAuthzStatusCode, &newAuthzBody, http.Header{"Replay-Nonce": {"nonce-3"}}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/acme/challenge/abcde/124"),
					ghttp.VerifyContentType("application/jose+jws"),
					ghttp.RespondWith(http.StatusAccepted, `{}`, http.Header{"Replay-Nonce": {"nonce-4"}}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/acme/challenge/abcde/124"),
					ghttp.RespondWithPtr(&challengeStatusCode, &challengeBody, http.Header{"Replay-Nonce": {"nonce-5"}}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/new-cert"),
					ghttp.VerifyContentType("application/jose+jws"),
					ghttp.RespondWith(
						http.StatusCreated,
						string(letsencryptPEM.Bytes),
						http.Header{
							"Replay-Nonce": {"nonce-6"},
							// Specify issuer certificate URL in the "up" Link.
							// We will need to make a request to this URL to create a
							// certificate bundle.
							"Link": {`<` + acmeServer.URL() + `/issuer-cert>;rel="up"`},
							// URI to get a renewed cert from.
							"Location": {acmeServer.URL() + `/renew-cert/deadbeef`},
						},
					),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/issuer-cert"),
					ghttp.RespondWith(http.StatusOK, string(letsencryptIssuerPEM.Bytes)),
				),
			)

			origAesKey = common.AesKey
			common.AesKey = "something-something-something-32"

			origAcmeURL = common.AcmeURL
			common.AcmeURL = acmeServer.URL()
		})

		AfterEach(func() {
			s3client.S3 = origS3
			acmeServer.Close()
			common.AesKey = origAesKey
			common.AcmeURL = origAcmeURL
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/"+proj.Name+"/domains/"+dm.Name+"/cert/letsencrypt", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		doRequestWithDefaultDomain := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/"+proj.Name+"/domains/"+proj.DefaultDomainName()+"/cert/letsencrypt", nil, headers, nil)
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

		It("returns 200 OK", func() {
			doRequest()

			Expect(res.StatusCode).To(Equal(http.StatusOK))

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
					"issuer": "%s"
				}
			}`, ct.ID, formattedTimeForJSON(ct.StartsAt), formattedTimeForJSON(ct.ExpiresAt), *ct.CommonName, *ct.Issuer)))
		})

		It("creates an ACME cert record for the domain", func() {
			acmeCert := &acmecert.AcmeCert{}
			err := db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(Equal(gorm.RecordNotFound))

			doRequest()

			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			Expect(acmeCert.DomainID).To(Equal(dm.ID))
			Expect(acmeCert.LetsencryptKey).NotTo(Equal(""))
			Expect(acmeCert.PrivateKey).NotTo(Equal(""))
			Expect(acmeCert.Cert).NotTo(Equal(""))
		})

		It("encrypts and saves the certificate returned from Let's Encrypted bundled with the issuer certificate", func() {
			doRequest()

			acmeCert := &acmecert.AcmeCert{}
			err := db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			certChain, err := acmeCert.DecryptedCerts(common.AesKey)
			Expect(err).To(BeNil())

			Expect(certChain).To(HaveLen(2))

			// The actual cert comes first in the chain.
			domainCert := certChain[0]
			Expect(domainCert.Raw).To(Equal(letsencryptPEM.Bytes))

			// Followed by the issuer cert.
			issuerCert := certChain[1]
			Expect(issuerCert.Raw).To(Equal(letsencryptIssuerPEM.Bytes))
		})

		It("uses an existing Let's Encrypt private key when there's one", func() {
			acmeCert, err := acmecert.New(dm.ID, common.AesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())

			key := acmeCert.LetsencryptKey

			doRequest()

			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())
			Expect(acmeCert.LetsencryptKey).To(Equal(key))
		})

		It("uploads Let's Encrypt certificate and private key to S3", func() {
			doRequest()
			Expect(fakeS3.UploadCalls.Count()).To(Equal(2))

			call := fakeS3.UploadCalls.NthCall(1)
			Expect(call).NotTo(BeNil())
			Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(call.Arguments[2]).To(Equal("certs/www.foo-bar-express.com/ssl.crt"))
			Expect(call.Arguments[4]).To(Equal(""))
			Expect(call.Arguments[5]).To(Equal("private"))
			encryptedCrt, ok := call.SideEffects["uploaded_content"].([]byte)
			Expect(ok).To(BeTrue())
			decryptedCrt, err := aesencrypter.Decrypt(encryptedCrt, []byte(common.AesKey))
			Expect(err).To(BeNil())
			bundledPEM := append(letsencryptCert, letsencryptIssuerCert...)
			Expect(decryptedCrt).To(Equal(bundledPEM))

			call = fakeS3.UploadCalls.NthCall(2)
			Expect(call).NotTo(BeNil())
			Expect(call.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(call.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(call.Arguments[2]).To(Equal("certs/www.foo-bar-express.com/ssl.key"))
			Expect(call.Arguments[4]).To(Equal(""))
			Expect(call.Arguments[5]).To(Equal("private"))
			encryptedKey, ok := call.SideEffects["uploaded_content"].([]byte)
			Expect(ok).To(BeTrue())
			decryptedKey, err := aesencrypter.Decrypt(encryptedKey, []byte(common.AesKey))
			Expect(err).To(BeNil())

			acmeCert := &acmecert.AcmeCert{}
			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			privKey, err := acmeCert.DecryptedPrivateKey(common.AesKey)
			Expect(err).To(BeNil())
			privKeyPEM := pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(privKey),
			})

			Expect(decryptedKey).To(Equal(privKeyPEM))
		})

		It("publishes invalidation message for the domain", func() {
			doRequest()

			d := testhelper.ConsumeQueue(mq, invalidationQueueName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{"domains": ["www.foo-bar-express.com"]}`))
		})

		It("saves Let's Encrypt's HTTP challenge details", func() {
			doRequest()

			acmeCert := &acmecert.AcmeCert{}
			err := db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			Expect(acmeCert.HTTPChallengePath).To(Equal("/.well-known/acme-challenge/secret-token"))
			Expect(acmeCert.HTTPChallengeResource).To((HavePrefix("secret-token.")))
		})

		It("saves the cert renewal URI returned by Let's Encrypt", func() {
			doRequest()

			acmeCert := &acmecert.AcmeCert{}
			err := db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			Expect(acmeCert.CertURI).To(Equal(acmeServer.URL() + `/renew-cert/deadbeef`))
		})

		It("tracks an Activated Let's Encrypt certificate event", func() {
			doRequest()

			trackCall := fakeTracker.TrackCalls.NthCall(1)
			Expect(trackCall).NotTo(BeNil())
			Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
			Expect(trackCall.Arguments[1]).To(Equal("Activated Let's Encrypt certificate"))
			Expect(trackCall.Arguments[2]).To(Equal(""))

			t := trackCall.Arguments[3]
			props, ok := t.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(props["projectName"]).To(Equal("foo-bar-express"))
			Expect(props["domain"]).To(Equal("www.foo-bar-express.com"))

			ct := &cert.Cert{}
			Expect(db.Last(ct).Error).To(BeNil())
			Expect(props["certId"]).To(Equal(ct.ID))
			Expect(props["certIssuer"]).To(Equal(ct.Issuer))
			Expect(props["certExpiresAt"]).To(Equal(ct.ExpiresAt))

			Expect(trackCall.Arguments[4]).To(BeNil())
			Expect(trackCall.ReturnValues[0]).To(BeNil())
		})

		Context("when a Let's Encrypt certificate has previously been setup", func() {
			var acmeCert = &acmecert.AcmeCert{}

			BeforeEach(func() {
				doRequest()
				Expect(res.StatusCode).To(Equal(http.StatusOK))

				err := db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
				Expect(err).To(BeNil())
			})

			It("returns HTTP 409 Conflict and does not update the certificate", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusConflict))

				acmeCert2 := &acmecert.AcmeCert{}
				err := db.Where("domain_id = ?", dm.ID).First(acmeCert2).Error
				Expect(err).To(BeNil())

				Expect(acmeCert).To(Equal(acmeCert2))
			})
		})

		Context("when Let's Encrypt fails to return a HTTP challenge", func() {
			BeforeEach(func() {
				newAuthzBody = `{
					"identifier": {
						"type": "dns",
						"value": "www.foo-bar-express.com"
					},
					"status": "pending",
					"expires": "2016-06-28T09:41:07.002634342Z",
					"challenges": [
						{
							"type": "tls-sni-01",
							"status": "pending",
							"uri": "` + acmeServer.URL() + `/acme/challenge/abcde/124",
							"token": "secret-token"
						}
					],
					"combinations": [
						[0],
						[1],
						[2]
					]
				}`
			})

			It("responds with HTTP 500", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusInternalServerError))
			})
		})

		Context("when Let's Encrypt is down", func() {
			BeforeEach(func() {
				crashedAcmeServer := ghttp.NewServer()
				common.AcmeURL = crashedAcmeServer.URL()
				crashedAcmeServer.Close()
			})

			It("responds with HTTP 503", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusServiceUnavailable))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(b.String()).To(MatchJSON(`{
					"error": "service_unavailable",
					"error_description": "could not connect to Let's Encrypt"
				}`))
			})
		})

		Context("when Let's Encrypt fails to verify the challenge", func() {
			BeforeEach(func() {
				challengeBody = `{
					"type": "http-01",
					"status": "invalid",
					"error": {
						"type": "urn:acme:error:connection",
						"detail": "DNS problem: NXDOMAIN looking up A for www.foo-bar-express.com",
						"status": 400
					},
					"uri": "https://acme-staging.api.letsencrypt.org/acme/challenge/G4HRIqRDdp3ZaSqEfs6QDlT5mKYznW1UVhKxVJ4uYEc/8957093",
					"token": "8tk3CajTQmTF7xca4IKKsHnEph0W5ojlbvnR07jeY2g",
					"keyAuthorization": "8tk3CajTQmTF7xca4IKKsHnEph0W5ojlbvnR07jeY2g.Ox2Ol9LwrRKfChQfiK5-o73S0MdiEEO9MAYPsfbIq8I",
					"validationRecord": [
						{
							"url": "http://www.foo-bar-express.com/.well-known/acme-challenge/8tk3CajTQmTF7xca4IKKsHnEph0W5ojlbvnR07jeY2g",
							"hostname": "www.foo-bar-express.com",
							"port": "80",
							"addressesResolved": null,
							"addressUsed": ""
						}
					]
				}`
			})

			It("responds with HTTP 503", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusServiceUnavailable))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(b.String()).To(MatchJSON(`{
					"error": "service_unavailable",
					"error_description": "domain could not be verified"
				}`))
			})
		})

		Context("when the default domain is given", func() {
			It("responds with HTTP 403 forbidden", func() {
				doRequestWithDefaultDomain()

				Expect(res.StatusCode).To(Equal(http.StatusForbidden))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(b.String()).To(MatchJSON(`{
					"error": "forbidden",
					"error_description": "the default domain is already secure"
				}`))
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

		Context("when the domain does not belong to the project", func() {
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

	Describe("DELETE /projects/:project_name/domains/:name/cert", func() {
		var (
			err    error
			fakeS3 *fake.S3
			origS3 filetransfer.FileTransfer

			mq    *amqp.Connection
			qName string

			u  *user.User
			oc *oauthclient.OauthClient
			t  *oauthtoken.OauthToken

			headers http.Header
			proj    *project.Project
			dm      *domain.Domain
			ct      *cert.Cert
		)

		BeforeEach(func() {
			origS3 = s3client.S3
			fakeS3 = &fake.S3{}
			s3client.S3 = fakeS3

			mq, err = mqconn.MQ()
			Expect(err).To(BeNil())

			testhelper.TruncateTables(db.DB())
			testhelper.DeleteQueue(mq, queues.All...)
			testhelper.DeleteExchange(mq, exchanges.All...)

			u, oc, t = factories.AuthTrio(db)

			proj = factories.Project(db, u, "foo-bar-express")
			dm = factories.Domain(db, proj, "www.foo-bar-express.com")
			ct = &cert.Cert{
				DomainID:        dm.ID,
				CertificatePath: "foo/bar",
				PrivateKeyPath:  "baz/qux",
				ExpiresAt:       time.Now().Add(365 * 24 * time.Hour),
				StartsAt:        time.Now().Add(-365 * 24 * time.Hour),
			}
			Expect(db.Create(ct).Error).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			qName = testhelper.StartQueueWithExchange(mq, exchanges.Edges, exchanges.RouteV1Invalidation)
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE", s.URL+"/projects/foo-bar-express/domains/www.foo-bar-express.com/cert", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("return 200 OK", func() {
			doRequest()
			Expect(res.StatusCode).To(Equal(http.StatusOK))

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(b.String()).To(MatchJSON(`{"deleted": true}`))
		})

		It("deletes ssl cert from DB", func() {
			doRequest()
			Expect(db.First(ct, ct.ID).Error).To(Equal(gorm.RecordNotFound))
		})

		It("deletes Let's Encrypt ACME cert from DB, if it exists", func() {
			aesKey := "something-something-something-32"
			acmeCert, err := acmecert.New(dm.ID, aesKey)
			Expect(err).To(BeNil())
			Expect(db.Create(acmeCert).Error).To(BeNil())

			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(BeNil())

			doRequest()

			err = db.Where("domain_id = ?", dm.ID).First(acmeCert).Error
			Expect(err).To(Equal(gorm.RecordNotFound))
		})

		It("deletes ssl cert from S3", func() {
			doRequest()

			Expect(fakeS3.DeleteCalls.Count()).To(Equal(1))

			deleteCall := fakeS3.DeleteCalls.NthCall(1)
			Expect(deleteCall).NotTo(BeNil())
			Expect(deleteCall.Arguments[0]).To(Equal(s3client.BucketRegion))
			Expect(deleteCall.Arguments[1]).To(Equal(s3client.BucketName))
			Expect(deleteCall.Arguments[2]).To(Equal("certs/www.foo-bar-express.com/ssl.crt"))
			Expect(deleteCall.Arguments[3]).To(Equal("certs/www.foo-bar-express.com/ssl.key"))
			Expect(deleteCall.ReturnValues[0]).To(BeNil())
		})

		It("invalidates domain cache", func() {
			doRequest()

			d := testhelper.ConsumeQueue(mq, qName)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(`{ "domains": ["www.foo-bar-express.com"] }`))
		})

		It("tracks a 'Deleted SSL Certificate' event", func() {
			doRequest()

			trackCall := fakeTracker.TrackCalls.NthCall(1)
			Expect(trackCall).NotTo(BeNil())
			Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
			Expect(trackCall.Arguments[1]).To(Equal("Deleted SSL Certificate"))
			Expect(trackCall.Arguments[2]).To(Equal(""))

			t := trackCall.Arguments[3]
			props, ok := t.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(props["projectName"]).To(Equal("foo-bar-express"))
			Expect(props["domain"]).To(Equal("www.foo-bar-express.com"))
			Expect(props["certId"]).To(Equal(ct.ID))

			Expect(trackCall.Arguments[4]).To(BeNil())
			Expect(trackCall.ReturnValues[0]).To(BeNil())
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

		Context("when the domain does not belong to the project", func() {
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

var letsencryptCert = []byte(`-----BEGIN CERTIFICATE-----
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

var letsencryptIssuerCert = []byte(`-----BEGIN CERTIFICATE-----
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
