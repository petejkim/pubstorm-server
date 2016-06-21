package acmecert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
)

type AcmeCert struct {
	gorm.Model

	DomainID uint

	// LetsencryptKey is the private key we pass to Let's Encrypt.
	// We generate a different private key for each domain so that each domain
	// has its own Let's Encrypt "account".
	// Some alternatives are:
	// 1. Use the same account for all domains (i.e. centralized Nitrous
	//    account - also fine, but per-domain is more flexible).
	// 2. Use the same account per user (tricky because collaborators can also
	//    add a Let's Encrypt cert to a domain).
	LetsencryptKey string

	PrivateKey string
	Cert       string

	HTTPChallengePath     string `sql:"column:http_challenge_path"`
	HTTPChallengeResource string `sql:"column:http_challenge_resource"`
}

// New returns a new AcmeCert with randomly generated private RSA private keys
// in LetsencryptKey and PrivateKey.
func New(domainID uint, aesKey string) (*AcmeCert, error) {
	crt := &AcmeCert{DomainID: domainID}

	var err error
	leKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	crt.LetsencryptKey, err = encryptPrivateKey(leKey, aesKey)
	if err != nil {
		return nil, err
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	crt.PrivateKey, err = encryptPrivateKey(privKey, aesKey)
	if err != nil {
		return nil, err
	}

	return crt, nil
}

// encryptPrivatekey converts an RSA private key to ASN.1 DER encoded form,
// encrypts it with the given AES key, and then Base64-encodes it.
func encryptPrivateKey(privKey *rsa.PrivateKey, aesKey string) (string, error) {
	// Convert private key to ASN.1 DER encoded form.
	privKeyBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	return encryptBase64(privKeyBytes, aesKey)
}

func decryptPrivateKey(privKey, aesKey string) (*rsa.PrivateKey, error) {
	decrypted, err := decryptBase64(privKey, aesKey)
	if err != nil {
		return nil, err
	}

	pk, err := ssh.ParseRawPrivateKey(decrypted)
	if err != nil {
		return nil, err
	}

	rpk, ok := pk.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not an RSA key")
	}

	return rpk, nil
}

func encryptBase64(data []byte, aesKey string) (string, error) {
	cipherText, err := aesencrypter.Encrypt(data, []byte(aesKey))
	if err != nil {
		return "", fmt.Errorf("acmecert.encryptBase64(): error encrypting data, err: %v", err)
	}

	return base64.StdEncoding.EncodeToString(cipherText), nil
}

func decryptBase64(data, aesKey string) ([]byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	cipherText, err := ioutil.ReadAll(decoder)
	if err != nil {
		return nil, err
	}

	return aesencrypter.Decrypt(cipherText, []byte(aesKey))
}

func (c *AcmeCert) SaveCert(db *gorm.DB, cert *x509.Certificate, aesKey string) error {
	certPEM := pem.EncodeToMemory(&pem.Block{Bytes: cert.Raw, Type: "CERTIFICATE"})

	b, err := encryptBase64(certPEM, aesKey)
	if err != nil {
		return err
	}

	c.Cert = b

	return db.Model(AcmeCert{}).Where("id = ?", c.ID).Update("cert", b).Error
}

func (c *AcmeCert) DecryptedCert(aesKey string) (*x509.Certificate, error) {
	decrypted, err := decryptBase64(c.Cert, aesKey)
	if err != nil {
		return nil, err
	}

	pemBlock, _ := pem.Decode(decrypted)

	return x509.ParseCertificate(pemBlock.Bytes)
}

func (c *AcmeCert) DecryptedLetsencryptKey(aesKey string) (*rsa.PrivateKey, error) {
	return decryptPrivateKey(c.LetsencryptKey, aesKey)
}

func (c *AcmeCert) DecryptedPrivateKey(aesKey string) (*rsa.PrivateKey, error) {
	return decryptPrivateKey(c.PrivateKey, aesKey)
}
