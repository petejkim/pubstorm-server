package users_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/common"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/user"
	"github.com/nitrous-io/rise-server/pkg/mailer"
	"github.com/nitrous-io/rise-server/server"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
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
		var (
			fakeMailer *fake.Mailer
			origMailer mailer.Mailer
		)

		BeforeEach(func() {
			origMailer = common.Mailer
			fakeMailer = &fake.Mailer{}
			common.Mailer = fakeMailer
		})

		AfterEach(func() {
			common.Mailer = origMailer
		})

		doRequest := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/users", params)
			Expect(err).To(BeNil())
		}

		Context("when all required fields are present", func() {
			var u *user.User

			BeforeEach(func() {
				doRequest(url.Values{
					"email":    {"foo@example.com"},
					"password": {"foobar"},
				})
				u = &user.User{}
				db.Last(u)
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
				Expect(u.Email).To(Equal("foo@example.com"))
				Expect(u.ConfirmedAt.Valid).To(BeFalse())
				Expect(u.ConfirmationCode).NotTo(HaveLen(0))

				var pwHashed bool
				err = db.Table("users").Where("id = ? AND encrypted_password = crypt('foobar', encrypted_password)", u.ID).Count(&pwHashed).Error
				Expect(err).To(BeNil())

				Expect(pwHashed).To(BeTrue())
			})

			It("sends an email with confirmation code to user", func() {
				Expect(fakeMailer.SendMailCalled).To(BeTrue())
				Expect(fakeMailer.From).To(Equal(common.MailerEmail))
				Expect(fakeMailer.Tos).To(Equal([]string{"foo@example.com"}))
				Expect(fakeMailer.ReplyTo).To(Equal(common.MailerEmail))

				Expect(fakeMailer.Subject).To(ContainSubstring("Please confirm"))
				Expect(fakeMailer.Body).To(ContainSubstring(u.ConfirmationCode))
				Expect(fakeMailer.HTML).To(ContainSubstring(u.ConfirmationCode))
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

	Describe("POST /user/confirm", func() {
		var u *user.User
		var params url.Values

		BeforeEach(func() {
			db, err = dbconn.DB()
			Expect(err).To(BeNil())
			testhelper.TruncateTables(db.DB())

			u = &user.User{Email: "foo@example.com", Password: "foobar"}
			err = u.Insert()
			Expect(err).To(BeNil())
			Expect(u.ID).NotTo(BeZero())
			Expect(u.ConfirmedAt.Valid).To(BeFalse())

			params = url.Values{
				"email":             {u.Email},
				"confirmation_code": {u.ConfirmationCode},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/user/confirm", params)
			Expect(err).To(BeNil())
		}

		Context("when invalid params are provided", func() {
			DescribeTable("it returns 422 and does not mark user as confirmed",
				func(setUp func(), message string) {
					setUp()
					doRequest()

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"confirmed": false,
						"error_description": "` + message + `"
					}`))

					err = db.Where("id = ?", u.ID).First(u).Error
					Expect(err).To(BeNil())

					Expect(u.ConfirmedAt.Valid).To(BeFalse())
				},

				Entry("require email", func() {
					params.Del("email")
				}, "email is required"),

				Entry("require confirmation code", func() {
					params.Del("confirmation_code")
				}, "confirmation_code is required"),

				Entry("validate presence of email", func() {
					params.Set("email", u.Email+"x")
				}, "invalid email or confirmation_code"),

				Entry("validate confirmation code", func() {
					params.Set("confirmation_code", u.ConfirmationCode+"x")
				}, "invalid email or confirmation_code"),
			)
		})

		Context("when the correct confirmation code is provided", func() {
			It("returns 200 and marks user as confirmd", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"confirmed": true
				}`))

				err = db.Where("id = ?", u.ID).First(u).Error
				Expect(err).To(BeNil())

				Expect(u.ConfirmedAt.Valid).To(BeTrue())
				Expect(u.ConfirmedAt.Time.Unix()).NotTo(BeZero())
			})
		})
	})
})
