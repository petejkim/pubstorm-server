package users_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/mailer"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"

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

	Describe("PUT /user", func() {
		var (
			u                *user.User
			oc               *oauthclient.OauthClient
			t                *oauthtoken.OauthToken
			existingPassword string
			newPassword      string
			params           url.Values
			headers          http.Header
		)

		BeforeEach(func() {
			existingPassword = "123456"
			newPassword = "a1b2c3"
			u = factories.UserWithPassword(db, existingPassword)
			oc = factories.OauthClient(db)
			t = &oauthtoken.OauthToken{
				UserID:        u.ID,
				OauthClientID: oc.ID,
			}

			err := db.Create(t).Error
			Expect(err).To(BeNil())

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}

			params = url.Values{
				"existing_password": {existingPassword},
				"password":          {newPassword},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("PUT", s.URL+"/user", params, headers, nil)
			Expect(err).To(BeNil())
		}

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		Context("when invalid params are provided", func() {
			DescribeTable("it returns 422 and does not update password",
				func(setUp func(), expectedBody string) {
					setUp()
					doRequest()

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(expectedBody))

					err = db.First(u, u.ID).Error
					Expect(err).To(BeNil())

					// old password should still work.
					existingUser, err := user.Authenticate(db, u.Email, existingPassword)
					Expect(err).To(BeNil())
					Expect(existingUser.ID).To(Equal(u.ID))
				},

				Entry("require existing_password", func() {
					params.Del("existing_password")
				}, `{
					"error": "invalid_params",
					"errors": { "existing_password": "is required" }
				}`),

				Entry("require password", func() {
					params.Del("password")
				}, `{
					"error": "invalid_params",
					"errors": { "password": "is required" }
				}`),

				Entry("validate password", func() {
					params.Set("password", "123")
				}, `{
					"error": "invalid_params",
					"errors": { "password": "is too short (min. 6 characters)" }
				}`),

				Entry("validate existing_password", func() {
					params.Set("existing_password", "foogoo")
				}, `{
					"error": "invalid_params",
					"errors": { "existing_password": "is incorrect" }
				}`),

				Entry("check if existing_password and password are same", func() {
					params.Set("password", existingPassword)
				}, `{
					"error": "invalid_params",
					"errors": { "password": "cannot be the same as the existing password" }
				}`),
			)
		})

		It("returns 200 OK and updates the password", func() {
			doRequest()
			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(`{
				"user": {
					"email": "` + u.Email + `",
					"name": "",
					"organization": ""
				}
			}`))

			existingUser, err := user.Authenticate(db, u.Email, newPassword)
			Expect(err).To(BeNil())
			Expect(existingUser.ID).To(Equal(u.ID))
		})

		It("deletes old token", func() {
			t1 := &oauthtoken.OauthToken{
				UserID:        u.ID,
				OauthClientID: oc.ID,
			}
			err := db.Create(t1).Error
			Expect(err).To(BeNil())

			// To make sure it does not delete other users' tokens
			_, _, t2 := factories.AuthTrio(db)

			doRequest()
			var currentToken oauthtoken.OauthToken
			Expect(db.First(&currentToken, t.ID).Error).To(Equal(gorm.RecordNotFound))
			Expect(db.First(&currentToken, t1.ID).Error).To(Equal(gorm.RecordNotFound))
			Expect(db.First(&currentToken, t2.ID).Error).To(BeNil())
		})
	})
})
