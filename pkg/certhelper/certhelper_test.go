package certhelper_test

import (
	"testing"

	"github.com/nitrous-io/rise-server/pkg/certhelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "certhelper")
}

var _ = Describe("CertHelper", func() {
	Describe("GetInfo", func() {
		var (
			certificate []byte
			privateKey  []byte
		)

		BeforeEach(func() {
			certificate = []byte(`-----BEGIN CERTIFICATE-----
MIIDqzCCApOgAwIBAgIJAMh/Miyzn6vjMA0GCSqGSIb3DQEBCwUAMGwxCzAJBgNV
BAYTAlVTMRMwEQYDVQQIDApDYWxpZm9ybmlhMRYwFAYDVQQHDA1TYW4gRnJhbmNp
c2NvMRkwFwYDVQQKDBBbREVWXSBuMm9kZXYuY29tMRUwEwYDVQQDDAwqLm4yb2Rl
di5jb20wHhcNMTQwOTE1MTgxODM1WhcNMTkwOTE1MTgxODM1WjBsMQswCQYDVQQG
EwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5jaXNj
bzEZMBcGA1UECgwQW0RFVl0gbjJvZGV2LmNvbTEVMBMGA1UEAwwMKi5uMm9kZXYu
Y29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnWxABUWSETnBZW5Z
vvXZKFwSfOM0nd+wIf7iT0dqo4bEzzQx9b9FDAJEdYlwZakLRrpQp0KlM5c5KNaY
9764UP/9WTjj8dHH1EZKMjEzZOi2uSHBZRROLel9Zb6DGofgaO63FuTV+g7SCUS4
e4CvKQkXujvBqXnYqHPb2TzonjgX5+JGZr3Ixxx2sYwd2IUatEP/NzOEE5hZYqsE
HPeSp2s4yZNYyjMuBLy/ZV+q7t72FOcPPh4oOR6673O1ASIudHZEuUSG/mnuTDd4
lauAB+vDbRzSjStA1mT/5qipVPiIm7lxdaUIdeQiIqnAiFvLepUsjkkQfeJR8/zb
/4kZBwIDAQABo1AwTjAdBgNVHQ4EFgQUekcqJ9g2+MHw7J+NpoB5kJGr0NkwHwYD
VR0jBBgwFoAUekcqJ9g2+MHw7J+NpoB5kJGr0NkwDAYDVR0TBAUwAwEB/zANBgkq
hkiG9w0BAQsFAAOCAQEAEXsUxMa8G4z9rriLLp2FdB8rnFOmhsIpTwrXYeHq93eb
LWP0Se7C5RC7zN+a6q7N+4Ru4pW0evk7crdjdP5O+E0OGrdUL0lw1lHYEba40rna
6HrtQOreEtwFu64zJm0fQHNIqVXYCd6SPPLWC8DA8o4vRthyxHp5e+1K3FkDt0FR
kobGOD21haji/y6hYl/Bt05VvWF5hQf75D6A0FJbcsrd+QkX+biYcEWZfQla8Uej
y1mn8kLnYvr9gG45dezObogeXMxfsGBQJKeibEBifapBQaDCd8BqqDe9buAm8t8J
KKx0YPZxvDe/mwPxsyjkyIdAVY2ZfsXY+MmmgH9gRg==
-----END CERTIFICATE-----`)
			privateKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAnWxABUWSETnBZW5ZvvXZKFwSfOM0nd+wIf7iT0dqo4bEzzQx
9b9FDAJEdYlwZakLRrpQp0KlM5c5KNaY9764UP/9WTjj8dHH1EZKMjEzZOi2uSHB
ZRROLel9Zb6DGofgaO63FuTV+g7SCUS4e4CvKQkXujvBqXnYqHPb2TzonjgX5+JG
Zr3Ixxx2sYwd2IUatEP/NzOEE5hZYqsEHPeSp2s4yZNYyjMuBLy/ZV+q7t72FOcP
Ph4oOR6673O1ASIudHZEuUSG/mnuTDd4lauAB+vDbRzSjStA1mT/5qipVPiIm7lx
daUIdeQiIqnAiFvLepUsjkkQfeJR8/zb/4kZBwIDAQABAoIBAGa3XUaTUG2g68nN
KQ3qyFkHSqDmd0yTyg9EilIEXVZ59yzj971LojflutmeZhJPLKZnp3ybhcOV5pv4
+jKc5RMlFSAEeOcuZF7jxkHdzJUJK0C8/71+dEyMz1914YGMKycMq7ZqdhwFU5Ls
nhsnqiLyZeMEXqbAdFfl0Qt8LKQfG62bD3hI60m6HAdS7Z8/IDhsEg/0rP5pLpMQ
tGibjgPxzDwAxw/QmnFW+9rBXaEnnSm3Pvi5OPLTBnPiSfvSqkwSs/w12VO8sYuC
vmydYYxMNNkh+rjAlTbYlqxsXJuW2jzEtNirtYPLiinE8i7k05/lx3sibm1UuyHZ
nLkrCxECgYEAyoMvdbNN30W0Z1dj7G7meiwTxzDg785jwIhNYVn5uLZRLnOIQNXh
N4qFH+IzP2pWjLnx5YIuvQBra/SVzCTq7s9wyhSmnr50WeHU5yQQ66jpQdaJPgxC
iiikr/lLApzxirhGuHThkyhGU70k1fWe9WpuK2LNx0/nkCfNN0iPZtsCgYEAxwBY
pnaWN5REmL+i8vWFxhhCvRI+VrWY5y407g23hEnG1lNU0YYxNuTRi0RnBtfxaj7O
XZcm+gawNja9mDhAEnojo6t9jut/+IjeADAr03jZep+r06h1+WKVmDQiLIg7vCVI
ixC7hNLP/5Mnkrn3yhLTcm5pnbAugKvwDNFFIEUCgYBaa8SvGwY0IN1yHvUAxmum
NTQHhm2I5XBosPNL+m6j6NPKl89Ik7bho7nZCJi1QfevEf9N6JiRzzQnmaeg5QL4
6iqEMEBNNOCimVEEe3gKoPq1aOMSj0rOgWM3J2o0mnrG44zAI3/swtjT3uoplmgJ
UCIswQr8aVMNbJgWjRFqbQKBgQCYpL1bQo9LJqHPgP+e2ZG5N5bJrJrArB8TBTB4
gXEJOgYZFGZ1KTfK4Y2SA+/7Idz+IBrvUygElOjJTQf1IQCUq7d2re5rmFza6TFQ
d6LGXWaEVsHYYtnLZ0FUNHkaK42WbgrNERKleYcuhVPPinJ1QCeNGQBOgnvJGxnQ
2xzo+QKBgQCFUcSnAv5WE2tbyPB+6FFps7a39RYdii911KPHB5gek+gTEGyEgND7
50XojNdgTE+ObcoovatkCns+TYRdKmP7BDsJdE2KsompTiaNMDCFhb6QRZ28hZVK
nqz5zr68zkEgxlfrZnBxifOvcdmlfGdhM3KSIGAzQOuyUaiblA+cqg==
-----END RSA PRIVATE KEY-----`)
		})

		It("returns cert info if it is valid", func() {
			cm, err := certhelper.GetInfo(certificate, privateKey, "www.n2odev.com")
			Expect(err).To(BeNil())
			Expect(cm).NotTo(BeNil())
			Expect(cm.StartsAt.String()).To(Equal("2014-09-15 18:18:35 +0000 UTC"))
			Expect(cm.ExpiresAt.String()).To(Equal("2019-09-15 18:18:35 +0000 UTC"))
			Expect(cm.CommonName).To(Equal("*.n2odev.com"))
		})

		It("returns an error if cert is valid but common name is not valid", func() {
			cm, err := certhelper.GetInfo(certificate, privateKey, "www.foo-bar-express.com")
			Expect(err).To(Equal(certhelper.ErrInvalidCommonName))
			Expect(cm).To(BeNil())
		})

		It("returns an error if wrong cert is given", func() {
			cm, err := certhelper.GetInfo([]byte("hello"), []byte("world"), "www.n2odev.com")
			Expect(err).To(Equal(certhelper.ErrInvalidCert))
			Expect(cm).To(BeNil())
		})
	})
})
