package users_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/user"
	"github.com/nitrous-io/rise-server/server"
	"github.com/nitrous-io/rise-server/testhelper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "users")
}

var _ = Describe("Users", func() {
	var (
		db  *gorm.DB
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

	Describe("POST /users", func() {
		doRequest := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/users", params)
			Expect(err).To(BeNil())
		}

		Context("when all required fields are present", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"email":    {"foo@example.com"},
					"password": {"foobar"},
				})
			})

			It("returns 201 created", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusCreated))
				Expect(b.String()).To(MatchJSON(`{
					"user": {
						"email": "foo@example.com",
						"name": "",
						"organization": ""
					}
				}`))
			})

			It("creates a user record in the DB", func() {
				u := &user.User{}
				db.Last(u)

				Expect(u.Email).To(Equal("foo@example.com"))

				var pwHashed bool
				err = db.Table("users").Where("id = ? AND encrypted_password = crypt('foobar', encrypted_password)", u.ID).Count(&pwHashed).Error
				Expect(err).To(BeNil())

				Expect(pwHashed).To(BeTrue())
			})
		})

		Context("when email field is missing", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"password": {"foobar"},
				})
			})

			It("returns 422 unprocessable entity", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"errors": {
						"email": "is required"
					}
				}`))
			})

			It("does not create a user record in the DB", func() {
				userCount := 0
				db.Table("users").Count(&userCount)
				Expect(userCount).To(BeZero())
			})
		})

		Context("when password field is missing", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"email": {"foo@example.com"},
				})
			})

			It("returns 422 unprocessable entity", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"errors": {
						"password": "is required"
					}
				}`))
			})

			It("does not create a user record in the DB", func() {
				userCount := 0
				db.Table("users").Count(&userCount)
				Expect(userCount).To(BeZero())
			})
		})

		Context("when a field is invalid", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"email":    {"foo@@example.com"},
					"password": {"foobar"},
				})
			})

			It("returns 422 unprocessable entity", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"errors": {
						"email": "is invalid"
					}
				}`))
			})

			It("does not create a user record in the DB", func() {
				userCount := 0
				db.Table("users").Count(&userCount)
				Expect(userCount).To(BeZero())
			})
		})
	})
})
