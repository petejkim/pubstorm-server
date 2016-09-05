package main

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"os/user"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/ericchiang/letsencrypt"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
		os.Setenv("RISE_ENV", riseEnv)
	}
}

const jobName = "renew-acme-certs"

var (
	fields          = log.Fields{"job": jobName}
	expiryThreshold = 30 * 24 * time.Hour // Renew certs that have < 30 days left

	numRenewed int
	numFailed  int
)

func main() {
	if u, err := user.Current(); err == nil {
		fields["user"] = u.Username
	}
	log.WithFields(fields).WithField("event", "start").
		Infof("Renewing ACME certificates that are about to expire...")

	db, err := dbconn.DB()
	if err != nil {
		log.WithFields(fields).Fatalf("failed to initialize db, err: %v", err)
	}

	earliestExpiresAt := time.Now().Add(expiryThreshold)
	acmeCerts, err := findExpiringAcmeCerts(db, earliestExpiresAt)
	if err != nil {
		log.WithFields(fields).Fatalf("failed to retrieve expiring Let's Encrypt certs from db, err: %v", err)
	}

	log.WithFields(fields).Infof("Found %d expiring Let's Encrypt certs", len(acmeCerts))

	var (
		wg       sync.WaitGroup
		jobs     = make(chan *acmecert.AcmeCert, len(acmeCerts))
		nWorkers = 3 // Max number of concurrent renewal requests at any one time.
	)

	for i := 0; i < nWorkers; i++ {
		go renewer(db, &wg, jobs)
	}

	for i, cert := range acmeCerts {
		log.WithFields(fields).Infof("[%d/%d] Adding to renewal queue: ACME cert ID %d",
			i+1, len(acmeCerts), cert.ID)
		wg.Add(1)
		jobs <- cert
	}

	wg.Wait()

	log.WithFields(fields).WithField("event", "completed").
		Infof("Attempted renewal of %d ACME certificates, success: %d, failed: %d", len(acmeCerts), numRenewed, numFailed)
}

// findExpiringAcmeCerts returns AcmeCerts that expire before the deadline.
func findExpiringAcmeCerts(db *gorm.DB, deadline time.Time) ([]*acmecert.AcmeCert, error) {
	acmeCerts := []*acmecert.AcmeCert{}

	certs := []*cert.Cert{}
	if err := db.Where("expires_at <= ?", deadline).Find(&certs).Error; err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return acmeCerts, nil
	}

	// Find Let's Encrypt certs that we can actually renew programmatically.
	domainIDs := make([]uint, 0, len(certs))
	for _, cert := range certs {
		domainIDs = append(domainIDs, cert.DomainID)
	}
	if err := db.Where("domain_id IN (?)", domainIDs).Find(&acmeCerts).Error; err != nil {
		return nil, err
	}

	return acmeCerts, nil
}

func renewer(db *gorm.DB, wg *sync.WaitGroup, jobs chan *acmecert.AcmeCert) {
	for cert := range jobs {
		log.WithFields(fields).Infof("Renewing ACME cert ID %d", cert.ID)

		if err := renew(db, cert); err != nil {
			log.WithFields(fields).Errorf("failed to renew ACME cert ID %d, err: %v", cert.ID, err)
			numFailed++
		} else {
			numRenewed++
		}

		wg.Done()
	}

}

func renew(db *gorm.DB, acmeCert *acmecert.AcmeCert) error {
	certChain, err := acmeCert.DecryptedCerts(common.AesKey)
	if err != nil {
		return fmt.Errorf("failed to decrypt ACME cert %d, err: %v", acmeCert.ID, err)
	}

	var dom domain.Domain
	if err := db.First(&dom, acmeCert.DomainID).Error; err != nil {
		return err
	}

	x509Cert := certChain[0]
	log.WithFields(fields).Infof("ACME cert %d for %q expires on %v", acmeCert.ID, dom.Name, x509Cert.NotAfter)

	cli, err := letsencrypt.NewClient(common.AcmeURL)
	if err != nil {
		return err
	}

	certResp, err := cli.RenewCertificate(acmeCert.CertURI)
	if err != nil {
		log.WithFields(fields).Errorf("error renewing certificate with Let's Encrypt, ACME cert ID: %d, err: %v",
			acmeCert.ID, err)
		return err
	}

	if certResp.Certificate == nil {
		log.WithFields(fields).Infof("ACME cert ID %d not available from Let's Encrypt yet, retry after %d seconds",
			acmeCert.ID, certResp.RetryAfter)
		return nil
	}

	log.WithFields(fields).Infof("Returned cert expires on %v", certResp.Certificate.NotAfter)

	if certResp.Certificate.Equal(x509Cert) {
		log.WithFields(fields).Infof("Let's Encrypt returned an identical cert for ACME cert ID %d - requesting a new cert instead...", acmeCert.ID)

		certKey, err := acmeCert.DecryptedPrivateKey(common.AesKey)
		if err != nil {
			return err
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
			return err
		}
		csr, err := x509.ParseCertificateRequest(csrDER)
		if err != nil {
			return err
		}

		leKey, err := acmeCert.DecryptedLetsencryptKey(common.AesKey)
		if err != nil {
			return err
		}

		certResp, err = cli.NewCertificate(leKey, csr)
		if err != nil {
			log.WithFields(fields).Errorf("failed to get new certificate from Let's Encrypt, domain: %q, err: %v", dom.Name, err)
			return err
		}

		log.WithFields(fields).Infof("Got new cert that expires on %v", certResp.Certificate.NotAfter)
	} else {
		log.WithFields(fields).Infof("Got renewed cert that expires on %v", certResp.Certificate.NotAfter)
	}

	// FIXME We should request for the issuer cert again (using
	// cli.Bundle(certResp)), in case it has changed.
	// But issue https://github.com/letsencrypt/boulder/issues/1218 (which is
	// still there on Let's Encrypt staging at this time) will cause the client
	// library we're using to fail because it'll make a request to
	// "/acme/issuer-cert", so for now we will re-use the old issuer cert.
	issuerCert := certChain[1]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certResp.Certificate.Raw})
	issuerPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerCert.Raw})
	bundledPEM := append(certPEM, issuerPEM...)

	acmeCert.CertURI = certResp.URI
	if err := db.Save(acmeCert).Error; err != nil {
		return err
	}

	if err := acmeCert.SaveCert(db, bundledPEM, common.AesKey); err != nil {
		return err
	}

	// Upload cert to S3.
	if err := uploadCert(dom.Name, bundledPEM); err != nil {
		return err
	}

	log.WithFields(fields).Infof("Uploaded renewed cert ID %d to S3", acmeCert.ID)

	var ct cert.Cert
	if err := db.Where("domain_id = ?", acmeCert.DomainID).Find(&ct).Error; err != nil {
		return err
	}

	ct.StartsAt = certResp.Certificate.NotBefore
	ct.ExpiresAt = certResp.Certificate.NotAfter
	if err := cert.Upsert(db, &ct); err != nil {
		return err
	}

	log.WithFields(fields).Infof("Successfully renewed cert ID %d", acmeCert.ID)

	return nil
}

func uploadCert(domainName string, cert []byte) error {
	certPath := fmt.Sprintf("certs/%s/ssl.crt", domainName) // TODO This should be a method of domain.Domain.
	encryptedCert, err := aesencrypter.Encrypt(cert, []byte(common.AesKey))
	if err != nil {
		return err
	}
	rdr := bytes.NewReader(encryptedCert)
	if err := s3client.Upload(certPath, rdr, "", "private"); err != nil {
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
