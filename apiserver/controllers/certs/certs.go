package certs

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/ericchiang/letsencrypt"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/pkg/certhelper"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

var MaxCertSize = int64(96 * 1024) // 96 kb

func Show(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	ct := &cert.Cert{}
	if err := db.Table("certs").
		Joins("JOIN domains ON domains.id = certs.domain_id").
		Where("domains.name = ? AND project_id = ?", domainName, proj.ID).First(ct).Error; err != nil {

		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "cert could not be found",
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cert": ct.AsJSON(),
	})
}

func Create(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	if strings.HasSuffix(domainName, shared.DefaultDomain) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "not allowed to upload certs for default domain",
		})
		return
	}

	// get the multipart reader for the request.
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "the request should be encoded in multipart/form-data format",
		})
		return
	}

	if n, err := strconv.ParseInt(c.Request.Header.Get("Content-Length"), 10, 64); err != nil || n > MaxCertSize {
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Content-Length header is required",
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "request body is too large",
			})
		}
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	d := &domain.Domain{}
	if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(d).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "domain could not be found",
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	ct := &cert.Cert{
		DomainID: d.ID,
	}

	var certBytes, pKeyBytes []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		if part.FormName() == "" {
			continue
		}

		switch part.FormName() {
		case "cert":
			certBytes, err = ioutil.ReadAll(part)
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			ct.CertificatePath = fmt.Sprintf("certs/%s/ssl.crt", domainName)
		case "key":
			pKeyBytes, err = ioutil.ReadAll(part)
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			ct.PrivateKeyPath = fmt.Sprintf("certs/%s/ssl.key", domainName)
		default:
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "unrecognized form field",
			})
			return
		}
	}

	if certBytes == nil || pKeyBytes == nil {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "both cert and key are required",
		})
		return
	}

	info, err := certhelper.GetInfo(certBytes, pKeyBytes, domainName)
	if err != nil {
		if err == certhelper.ErrInvalidCert {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "invalid cert or key",
			})
		} else if err == certhelper.ErrInvalidCommonName {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "invalid common name (domain name mismatch)",
			})
		} else {
			controllers.InternalServerError(c, err)
		}
		return
	}

	ct.StartsAt = info.StartsAt
	ct.ExpiresAt = info.ExpiresAt
	ct.CommonName = &info.CommonName
	ct.Issuer = &info.Issuer
	ct.Subject = &info.Subject

	if err := uploadCert(domainName, certBytes, pKeyBytes); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := cert.Upsert(db, ct); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		u := controllers.CurrentUser(c)

		var (
			event = "Uploaded SSL Certificate"
			props = map[string]interface{}{
				"projectName":   proj.Name,
				"domain":        d.Name,
				"certId":        ct.ID,
				"certSize":      len(certBytes),
				"certKeySize":   len(pKeyBytes),
				"certIssuer":    ct.Issuer,
				"certExpiresAt": ct.ExpiresAt,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"cert": ct.AsJSON(),
	})
}

func LetsEncrypt(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	// Disallow adding a Let's Encrypt cert for default domains since we will
	// always deploy a wildcard cert to secure them.
	if strings.HasSuffix(domainName, shared.DefaultDomain) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "the default domain is already secure",
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var dom domain.Domain
	if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(&dom).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "domain could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	tx := db.Begin()
	if err := tx.Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	defer tx.Rollback()

	acmeCert := &acmecert.AcmeCert{}
	if err := tx.Where("domain_id = ?", dom.ID).First(acmeCert).Error; err != nil {
		if err != gorm.RecordNotFound {
			controllers.InternalServerError(c, err)
			return
		}

		// If no record exists, create one.
		var err error
		acmeCert, err = acmecert.New(dom.ID, common.AesKey)
		if err != nil {
			log.Errorf("failed to initialize new AcmeCert for domain %q, err: %v", dom.Name, err)
			controllers.InternalServerError(c, err)
			return
		}

		if err := tx.Create(acmeCert).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		if err := tx.Commit().Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}
	}

	cli, err := letsencrypt.NewClient(common.AcmeURL)
	if err != nil {
		log.Errorf("failed to query Let's Encrypt directory %q, err: %v", common.AcmeURL, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":             "service_unavailable",
			"error_description": "could not connect to Let's Encrypt",
		})
		return
	}

	leKey, err := acmeCert.DecryptedLetsencryptKey(common.AesKey)
	if err != nil {
		log.Errorf("failed to decrypt Let's Encrypt private key, domain: %q, err: %v", dom.Name, err)
		controllers.InternalServerError(c, err)
		return
	}

	if _, err := cli.NewRegistration(leKey); err != nil {
		log.Errorf("failed to get Let's Encrypt registration, domain: %q, err: %v", dom.Name, err)
		controllers.InternalServerError(c, err)
		return
	}

	auth, _, err := cli.NewAuthorization(leKey, "dns", dom.Name)
	if err != nil {
		log.Errorf("failed to get Let's Encrypt challenges, domain: %q, err: %v", dom.Name, err)
		controllers.InternalServerError(c, err)
		return
	}

	// Get the HTTP ("http-01") challenge.
	var httpChallenge *letsencrypt.Challenge
	for _, chal := range auth.Challenges {
		if chal.Type == letsencrypt.ChallengeHTTP {
			httpChallenge = &chal
			break
		}
	}
	if httpChallenge == nil {
		log.Errorf("Let's Encrypt did not return a HTTP challenge, domain: %q, err: %v", dom.Name, err)
		controllers.InternalServerError(c, err)
		return
	}

	path, resource, err := httpChallenge.HTTP(leKey)
	if err != nil {
		log.Errorf("failed to get Let's Encrypt HTTP challenge details, domain: %q, err: %v", dom.Name, err)
		controllers.InternalServerError(c, err)
		return
	}

	// Save challenge details to database so that we can respond to Let's
	// Encrypt's verification request later.
	acmeCert.HTTPChallengePath = path
	acmeCert.HTTPChallengeResource = resource
	if err := db.Save(acmeCert).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	// Tell Let's Encrypt that we are ready for them to verify our response to
	// the HTTP challenge.
	// The ChallengeReady() method polls for 30s.
	if err := cli.ChallengeReady(leKey, *httpChallenge); err != nil {
		log.Errorf("failed to verify Let's Encrypt HTTP challenge, domain: %q, err: %v", dom.Name, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":             "service_unavailable",
			"error_description": "domain could not be verified",
		})
		return
	}

	// Now that Let's Encrypt has verified that we are legit owners of the
	// domain, we can finally request a certificate with a certificate signing
	// request (CSR).
	certKey, err := acmeCert.DecryptedPrivateKey(common.AesKey)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	template := &x509.CertificateRequest{
		SignatureAlgorithm: x509.SHA256WithRSA,
		PublicKeyAlgorithm: x509.RSA,
		PublicKey:          certKey.Public(),
		Subject:            pkix.Name{CommonName: dom.Name},
		DNSNames:           []string{dom.Name},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, certKey)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	certResp, err := cli.NewCertificate(leKey, csr)
	if err != nil {
		log.Errorf("failed to get certificate from Let's Encrypt, domain: %q, err: %v", dom.Name, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":             "service_unavailable",
			"error_description": "could not obtain a certificate from Let's Encrypt",
		})
		return
	}

	// Bundle cert with issuer cert.
	bundledPEM, err := cli.Bundle(certResp)
	if err != nil {
		log.Errorf("failed to get issuer certificate from Let's Encrypt, domain: %q, err: %v", dom.Name, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":             "service_unavailable",
			"error_description": "could not obtain issuer certificate from Let's Encrypt",
		})
		return
	}

	// Save cert to database so we can use it elsewhere (e.g. for renewals).
	if err := acmeCert.SaveCert(db, bundledPEM, common.AesKey); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	// Upload cert and its private key to S3.
	certKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certKey),
	})
	if err := uploadCert(dom.Name, bundledPEM, certKeyPEM); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	ct := &cert.Cert{
		DomainID:        dom.ID,
		CertificatePath: fmt.Sprintf("certs/%s/ssl.crt", dom.Name),
		PrivateKeyPath:  fmt.Sprintf("certs/%s/ssl.key", dom.Name),
		StartsAt:        certResp.Certificate.NotBefore,
		ExpiresAt:       certResp.Certificate.NotAfter,
		CommonName:      &certResp.Certificate.Subject.CommonName,
		Issuer:          &certResp.Certificate.Issuer.CommonName,
	}
	if err := cert.Upsert(db, ct); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		u := controllers.CurrentUser(c)

		var (
			event = "Activated Let's Encrypt certificate"
			props = map[string]interface{}{
				"projectName":   proj.Name,
				"domain":        dom.Name,
				"certId":        ct.ID,
				"certIssuer":    ct.Issuer,
				"certExpiresAt": ct.ExpiresAt,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"cert": ct.AsJSON(),
	})
}

func uploadCert(domainName string, cert, key []byte) error {
	certPath := fmt.Sprintf("certs/%s/ssl.crt", domainName)
	encryptedCert, err := aesencrypter.Encrypt(cert, []byte(common.AesKey))
	if err != nil {
		return err
	}
	rdr := bytes.NewReader(encryptedCert)
	if err := s3client.Upload(certPath, rdr, "", "private"); err != nil {
		return err
	}

	keyPath := fmt.Sprintf("certs/%s/ssl.key", domainName)
	encryptedKey, err := aesencrypter.Encrypt(key, []byte(common.AesKey))
	if err != nil {
		return err
	}
	rdr = bytes.NewReader(encryptedKey)
	if err := s3client.Upload(keyPath, rdr, "", "private"); err != nil {
		return err
	}

	// Invalidate cert cache
	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: []string{domainName},
	})
	if err != nil {
		return err
	}

	return m.Publish()
}

func Destroy(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var d domain.Domain
	if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(&d).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "cert could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	var crt cert.Cert
	if err := db.Where("domain_id = ?", d.ID).First(&crt).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "cert could not be found",
			})
			return
		}
	}

	if err := db.Delete(&crt).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	certificatePath := "certs/" + domainName + "/ssl.crt"
	privateKeyPath := "certs/" + domainName + "/ssl.key"
	if err := s3client.Delete(certificatePath, privateKeyPath); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: []string{domainName},
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := m.Publish(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		u := controllers.CurrentUser(c)

		var (
			event = "Deleted SSL Certificate"
			props = map[string]interface{}{
				"projectName": proj.Name,
				"domain":      d.Name,
				"certId":      crt.ID,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
	})
}
