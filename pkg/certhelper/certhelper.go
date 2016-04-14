package certhelper

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
)

type CertInfo struct {
	ExpiresAt time.Time
	StartsAt  time.Time

	CommonName string
}

var (
	ErrInvalidCert       = errors.New("invalid cert")
	ErrInvalidCommonName = errors.New("invalid common name")
)

func GetInfo(cert, pKey []byte, domainName string) (*CertInfo, error) {
	certificate, err := tls.X509KeyPair(cert, pKey)
	if err != nil {
		return nil, ErrInvalidCert
	}

	x509Cert, err := x509.ParseCertificate(certificate.Certificate[0])
	if err := x509Cert.VerifyHostname(domainName); err != nil {
		return nil, ErrInvalidCommonName
	}

	cm := &CertInfo{
		ExpiresAt:  x509Cert.NotAfter,
		StartsAt:   x509Cert.NotBefore,
		CommonName: x509Cert.Subject.CommonName,
	}

	return cm, nil
}
