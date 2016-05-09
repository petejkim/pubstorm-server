package aesencrypter_test

import (
	"testing"

	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "aesencrypter")
}

var _ = Describe("AESEncrypter", func() {
	var (
		key  = []byte("supercalifragilisticexpi") // 192-bit (24 byte) key
		data = []byte("super secret information")

		e1, e2, e3 []byte
		d1, d2, d3 []byte
		err        error
	)

	BeforeEach(func() {
		e1, err = aesencrypter.Encrypt(data, key)
		Expect(err).To(BeNil())
		e2, err = aesencrypter.Encrypt(data, key)
		Expect(err).To(BeNil())
		e3, err = aesencrypter.Encrypt(data, key)

		Expect(err).To(BeNil())
	})

	It("generates unique encryption each time", func() {
		Expect(e1).NotTo(Equal(data))
		Expect(e2).NotTo(Equal(data))
		Expect(e3).NotTo(Equal(data))

		Expect(e1).NotTo(Equal(e2))
		Expect(e1).NotTo(Equal(e3))
		Expect(e2).NotTo(Equal(e3))
	})

	It("can decrypt encrypted data", func() {
		e1, err = aesencrypter.Encrypt(data, key)
		Expect(err).To(BeNil())

		e2, err = aesencrypter.Encrypt(data, key)
		Expect(err).To(BeNil())

		e3, err = aesencrypter.Encrypt(data, key)
		Expect(err).To(BeNil())

		d1, err = aesencrypter.Decrypt(e1, key)
		Expect(err).To(BeNil())

		d2, err = aesencrypter.Decrypt(e2, key)
		Expect(err).To(BeNil())

		d3, err = aesencrypter.Decrypt(e3, key)
		Expect(err).To(BeNil())

		Expect(d1).To(Equal(data))
		Expect(d2).To(Equal(data))
		Expect(d2).To(Equal(data))
	})

	Context("when a key shorter than 24 bytes (192-bit) is given", func() {
		It("returns an error when attempting to encrypt", func() {
			_, err := aesencrypter.Encrypt(data, []byte("128bit-short-key"))
			Expect(err).To(Equal(aesencrypter.ErrKeyTooShort))
		})
	})

	Context("when a ciphertext shorter than 16 bytes is given (IV prefix is 16 bytes long)", func() {
		It("returns an error when attempting to decrypt", func() {
			_, err := aesencrypter.Decrypt(e1[0:15], key)
			Expect(err).To(Equal(aesencrypter.ErrCipherTextTooShort))
		})
	})
})
