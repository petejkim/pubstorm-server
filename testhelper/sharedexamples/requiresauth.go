package sharedexamples

import (
	"bytes"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/models/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func ItRequiresAuthentication(
	varFn func() (*gorm.DB, *user.User, *http.Header),
	reqFn func() *http.Response,
	assertFn func(),
) {
	var (
		db      *gorm.DB
		u       *user.User
		headers *http.Header

		res *http.Response
	)

	BeforeEach(func() {
		db, u, headers = varFn()
	})

	Context("when the Authorization header is missing", func() {
		BeforeEach(func() {
			headers.Del("Authorization")
			res = reqFn()
		})

		It("returns 401 unauthorized", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(b.String()).To(MatchJSON(`{
				"error": "invalid_token",
				"error_description": "access token is required"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})

	Context("when a non-existent token is given", func() {
		BeforeEach(func() {
			headers.Set("Authorization", headers.Get("Authorization")+"xxx")
			res = reqFn()
		})

		It("returns 401 unauthorized", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(b.String()).To(MatchJSON(`{
				"error": "invalid_token",
				"error_description": "access token is invalid"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})

	Context("when user does not exist", func() {
		BeforeEach(func() {
			err := db.Delete(u).Error
			Expect(err).To(BeNil())
			res = reqFn()
		})

		It("returns 401 unauthorized", func() {
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(b.String()).To(MatchJSON(`{
				"error": "invalid_token",
				"error_description": "access token is invalid"
			}`))

			if assertFn != nil {
				assertFn()
			}
		})
	})
}
