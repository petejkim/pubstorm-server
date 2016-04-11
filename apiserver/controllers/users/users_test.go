package users_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/mailer"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
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
				err := db.Last(u).Error
				Expect(err).To(BeNil())
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
				Expect(u.ConfirmedAt).To(BeNil())
				Expect(u.ConfirmationCode).NotTo(HaveLen(0))

				var pwHashed bool
				err = db.Model(user.User{}).Where("id = ? AND encrypted_password = crypt('foobar', encrypted_password)", u.ID).Count(&pwHashed).Error
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
				db.Model(user.User{}).Count(&userCount)
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
				db.Model(user.User{}).Count(&userCount)
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
				db.Model(user.User{}).Count(&userCount)
				Expect(userCount).To(BeZero())
			})
		})

		Context("when email is taken", func() {
			BeforeEach(func() {
				db, err = dbconn.DB()
				Expect(err).To(BeNil())
				testhelper.TruncateTables(db.DB())

				u := &user.User{Email: "foo@example.com", Password: "foobar"}
				err = u.Insert(db)
				Expect(err).To(BeNil())

				doRequest(url.Values{
					"email":    {"foo@example.com"},
					"password": {"foobar"},
				})
			})

			It("returns 422", func() {
				Expect(res.StatusCode).To(Equal(422))

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"errors": {
						"email": "is taken"
					}
				}`))
			})
		})
	})

	Describe("POST /user/confirm", func() {
		var u *user.User
		var params url.Values

		BeforeEach(func() {
			u = &user.User{Email: "foo@example.com", Password: "foobar"}
			err = u.Insert(db)
			Expect(err).To(BeNil())
			Expect(u.ID).NotTo(BeZero())
			Expect(u.ConfirmedAt).To(BeNil())

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

					err = db.First(u, u.ID).Error
					Expect(err).To(BeNil())

					Expect(u.ConfirmedAt).To(BeNil())
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

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				Expect(u.ConfirmedAt).NotTo(BeNil())
				Expect(*u.ConfirmedAt).NotTo(BeZero())
			})
		})
	})

	Describe("POST /user/confirm/resend", func() {
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
			res, err = http.PostForm(s.URL+"/user/confirm/resend", params)
			Expect(err).To(BeNil())
		}

		Context("when email address is not provided", func() {
			var u *user.User

			BeforeEach(func() {
				u = &user.User{Email: "foo@example.com", Password: "foobar"}
				err = u.Insert(db)
				Expect(err).To(BeNil())
				Expect(u.ID).NotTo(BeZero())
				Expect(u.ConfirmedAt).To(BeNil())

				doRequest(url.Values{})
			})

			It("returns 422 and does not send confirmation code via email", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error":             "invalid_params",
					"error_description": "email is required",
					"sent": false
				}`))

				Expect(fakeMailer.SendMailCalled).To(BeFalse())
			})
		})

		Context("when email address is provided", func() {
			var (
				u      *user.User
				params url.Values
			)

			BeforeEach(func() {
				params = url.Values{
					"email": {"foo@example.com"},
				}
			})

			Context("when user exists", func() {
				BeforeEach(func() {
					u = &user.User{Email: "foo@example.com", Password: "foobar"}
					err = u.Insert(db)
					Expect(err).To(BeNil())
					Expect(u.ID).NotTo(BeZero())
					Expect(u.ConfirmedAt).To(BeNil())
				})

				It("returns 200 OK", func() {
					doRequest(params)
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(http.StatusOK))
					Expect(b.String()).To(MatchJSON(`{
						"sent": true
					}`))
				})

				It("sends an email with confirmation code to user", func() {
					doRequest(params)
					Expect(fakeMailer.SendMailCalled).To(BeTrue())

					Expect(fakeMailer.From).To(Equal(common.MailerEmail))
					Expect(fakeMailer.Tos).To(Equal([]string{"foo@example.com"}))
					Expect(fakeMailer.ReplyTo).To(Equal(common.MailerEmail))

					Expect(fakeMailer.Subject).To(ContainSubstring("Please confirm"))
					Expect(fakeMailer.Body).To(ContainSubstring(u.ConfirmationCode))
					Expect(fakeMailer.HTML).To(ContainSubstring(u.ConfirmationCode))
				})

				Context("the user is already confirmed", func() {
					BeforeEach(func() {
						confirmed, err := user.Confirm(db, u.Email, u.ConfirmationCode)
						Expect(confirmed).To(BeTrue())
						Expect(err).To(BeNil())
					})

					It("return 422 and does not send an email", func() {
						doRequest(params)
						b := &bytes.Buffer{}
						_, err := b.ReadFrom(res.Body)
						Expect(err).To(BeNil())

						Expect(res.StatusCode).To(Equal(422))
						Expect(b.String()).To(MatchJSON(`{
							"error": "invalid_params",
							"error_description": "email is not found or already confirmed",
							"sent": false
						}`))
						Expect(fakeMailer.SendMailCalled).To(BeFalse())
					})
				})
			})

			Context("when user does not exist", func() {
				BeforeEach(func() {
					doRequest(params)
				})

				It("returns 422 and does not send confirmation code via email", func() {
					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"error_description": "email is not found or already confirmed",
						"sent": false
					}`))

					Expect(fakeMailer.SendMailCalled).To(BeFalse())
				})
			})
		})
	})

	Describe("POST /user/password/forgot", func() {
		var (
			fakeMailer *fake.Mailer
			origMailer mailer.Mailer
			u          *user.User
		)

		BeforeEach(func() {
			origMailer = common.Mailer
			fakeMailer = &fake.Mailer{}
			common.Mailer = fakeMailer
			u = factories.User(db)
		})

		AfterEach(func() {
			common.Mailer = origMailer
		})

		doRequest := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/user/password/forgot", params)
			Expect(err).To(BeNil())
		}

		It("returns 200 OK", func() {
			doRequest(url.Values{"email": {u.Email}})

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(`{
				"sent": true
			}`))
		})

		It("sends an email with a newly generated password reset token to the user", func() {
			Expect(u.PasswordResetToken).To(BeEmpty())

			doRequest(url.Values{"email": {u.Email}})

			u2 := &user.User{}
			err := db.First(&u2, u.ID).Error
			Expect(err).To(BeNil())

			Expect(u2.ID).To(Equal(u.ID))
			Expect(u2.PasswordResetToken).NotTo(BeEmpty())

			Expect(fakeMailer.SendMailCalled).To(BeTrue())

			Expect(fakeMailer.From).To(Equal(common.MailerEmail))
			Expect(fakeMailer.Tos).To(Equal([]string{u.Email}))
			Expect(fakeMailer.ReplyTo).To(Equal(common.MailerEmail))

			Expect(fakeMailer.Subject).To(ContainSubstring("password reset"))
			Expect(fakeMailer.Body).To(ContainSubstring(u.PasswordResetToken))
			Expect(fakeMailer.HTML).To(ContainSubstring(u.PasswordResetToken))
		})

		Context("when email address is not provided", func() {
			It("returns 422 and does not send password reset token via email", func() {
				doRequest(url.Values{})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "email is required",
					"sent": false
				}`))

				Expect(fakeMailer.SendMailCalled).To(BeFalse())
			})
		})

		Context("when there's no user with the given email address", func() {
			It("returns 200 OK but does not send a password reset token", func() {
				doRequest(url.Values{"email": {u.Email + "x"}})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"sent": true
				}`))

				Expect(fakeMailer.SendMailCalled).To(BeFalse())
			})
		})
	})

	Describe("POST /user/password/reset", func() {
		var u *user.User

		BeforeEach(func() {
			u = factories.User(db)
		})

		doRequest := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/user/password/reset", params)
			Expect(err).To(BeNil())
		}

		Context("when any invalid params are provided", func() {
			var params url.Values

			BeforeEach(func() {
				params = url.Values{
					"email":        {u.Email},
					"reset_token":  {"some-reset-token"},
					"new_password": {"new-password"},
				}
			})

			DescribeTable("it returns 422 and does not change the user's password",
				func(setUp func(), message string) {
					origPassword := u.Password

					setUp()
					doRequest(params)

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						"error_description": "` + message + `",
						"reset": false
					}`))

					err = db.First(u, u.ID).Error
					Expect(err).To(BeNil())

					u2, err := user.Authenticate(db, u.Email, origPassword)
					Expect(err).To(BeNil())
					Expect(u2).NotTo(BeNil())
					Expect(u2.ID).To(Equal(u.ID))
				},

				Entry("requires email", func() {
					params.Del("email")
				}, "email is required"),

				Entry("validates presence of user with given email", func() {
					params.Set("email", u.Email+"x")
				}, "email is not found"),

				Entry("requires password reset token", func() {
					params.Del("reset_token")
				}, "reset_token is required"),

				Entry("requires new password", func() {
					params.Del("new_password")
				}, "new_password is required"),
			)
		})

		Context("when the new password is in an invalid format", func() {
			It("returns 422 and does not change the user's password", func() {
				origPassword := u.Password
				newPassword := "x"

				doRequest(url.Values{
					"email":        {u.Email},
					"reset_token":  {"some-reset-token"},
					"new_password": {newPassword},
				})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())
				Expect(res.StatusCode).To(Equal(422))

				// I'm not sure how to test this in a cleaner fashion.
				u.Password = newPassword
				j := map[string]interface{}{
					"error":  "invalid_params",
					"errors": u.Validate(),
					"reset":  false,
				}
				expectedJSON, err := json.Marshal(j)
				Expect(err).To(BeNil())
				Expect(b.String()).To(MatchJSON(expectedJSON))

				u2, err := user.Authenticate(db, u.Email, origPassword)
				Expect(err).To(BeNil())
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
			})
		})

		Context("when user has not requested a password reset", func() {
			BeforeEach(func() {
				Expect(u.PasswordResetToken).To(BeEmpty())
			})

			It("returns 403 and does not change the user's password", func() {
				origPassword := u.Password

				doRequest(url.Values{
					"email":        {u.Email},
					"reset_token":  {"some-reset-token"},
					"new_password": {"new-password"},
				})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusForbidden))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "reset_token is incorrect",
					"reset": false
				}`))

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				u2, err := user.Authenticate(db, u.Email, origPassword)
				Expect(err).To(BeNil())
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
			})
		})

		Context("when user has requested a password reset", func() {
			BeforeEach(func() {
				err := u.GeneratePasswordResetToken(db)
				Expect(err).To(BeNil())
				Expect(u.PasswordResetToken).NotTo(BeEmpty())
			})

			It("returns 200 and changes the user's password", func() {
				newPassword := "new-password"

				doRequest(url.Values{
					"email":        {u.Email},
					"reset_token":  {u.PasswordResetToken},
					"new_password": {newPassword},
				})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{ "reset": true }`))

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				u2, err := user.Authenticate(db, u.Email, newPassword)
				Expect(err).To(BeNil())
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
			})
		})
	})
})
