package certhelper

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"strings"
	"time"
)

type CertInfo struct {
	ExpiresAt time.Time
	StartsAt  time.Time

	CommonName string

	Issuer  string
	Subject string
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

		Issuer:  stringifyNameData(x509Cert.Issuer),
		Subject: stringifyNameData(x509Cert.Subject),
	}

	return cm, nil
}

// https://tools.ietf.org/html/rfc4211
func stringifyNameData(n pkix.Name) string {
	d := make([]string, 0,
		len(n.Country)+
			len(n.Organization)+
			len(n.OrganizationalUnit)+
			len(n.Locality)+
			len(n.Province)+
			len(n.StreetAddress)+
			len(n.PostalCode)+
			2)

	for _, v := range n.Country {
		d = append(d, "C="+v)
	}

	for _, v := range n.Organization {
		d = append(d, "O="+v)
	}

	for _, v := range n.OrganizationalUnit {
		d = append(d, "OU="+v)
	}

	for _, v := range n.Locality {
		d = append(d, "L="+v)
	}

	for _, v := range n.Province {
		d = append(d, "ST="+v)
	}

	for _, v := range n.StreetAddress {
		d = append(d, "STREET="+v)
	}

	for _, v := range n.PostalCode {
		d = append(d, "PC="+v)
	}

	if n.SerialNumber != "" {
		d = append(d, "SERIALNUMBER="+n.SerialNumber)
	}

	if n.CommonName != "" {
		d = append(d, "CN="+n.CommonName)
	}

	return "/" + strings.Join(d, "/")
}
