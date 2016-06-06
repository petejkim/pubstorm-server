package users_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"github.com/nitrous-io/rise-server/pkg/tracker"
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
			params url.Values

			fakeMailer *fake.Mailer
			origMailer mailer.Mailer

			fakeTracker *fake.Tracker
			origTracker tracker.Trackable
		)

		BeforeEach(func() {
			origMailer = common.Mailer
			fakeMailer = &fake.Mailer{}
			common.Mailer = fakeMailer

			origTracker = common.Tracker
			fakeTracker = &fake.Tracker{}
			common.Tracker = fakeTracker

			params = url.Values{
				"email":    {"foo@example.com"},
				"password": {"foobar"},
			}
		})

		AfterEach(func() {
			common.Mailer = origMailer
			common.Tracker = origTracker
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = http.PostForm(s.URL+"/users", params)
			Expect(err).To(BeNil())
		}

		Context("when all required fields are present", func() {
			var u *user.User

			BeforeEach(func() {
				doRequest()
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

			It("tracks the new user", func() {
				identifyCall := fakeTracker.IdentifyCalls.NthCall(1)
				Expect(identifyCall).NotTo(BeNil())
				Expect(identifyCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))

				t := identifyCall.Arguments[1]
				traits, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(traits["email"]).To(Equal(u.Email))

				Expect(identifyCall.Arguments[2]).To(BeNil())
				Expect(identifyCall.ReturnValues[0]).To(BeNil())
			})
		})

		DescribeTable("missing or invalid params",
			func(setUp func(), expectedBody string) {
				setUp()

				var initialUserCount int
				Expect(db.Model(user.User{}).Count(&initialUserCount).Error).To(BeNil())

				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(expectedBody))

				var userCount int
				Expect(db.Model(user.User{}).Count(&userCount).Error).To(BeNil())
				Expect(initialUserCount - userCount).To(BeZero())
			},
			Entry("missing email", func() {
				params.Del("email")
			}, `{
				"error": "invalid_params",
				"errors": {
					"email": "is required"
				}
			}`),
			Entry("missing password", func() {
				params.Del("password")
			}, `{
				"error": "invalid_params",
				"errors": {
					"password": "is required"
				}
			}`),
			Entry("invalid email", func() {
				params.Set("email", "foo@@example.com")
			}, `{
				"error": "invalid_params",
				"errors": {
					"email": "is invalid"
				}
			}`),
			Entry("taken email", func() {
				u := &user.User{Email: "foo@example.com", Password: "foobar"}
				err = u.Insert(db)
				Expect(err).To(BeNil())
			}, `{
				"error": "invalid_params",
				"errors": {
					"email": "is taken"
				}
			}`),
			Entry("blacklisted email", func() {
				factories.BlacklistedEmail(db, "example.com")
			}, `{
				"error": "invalid_params",
				"errors": {
					"email": "is blacklisted"
				}
			}`),
		)
	})

	Describe("POST /user/confirm", func() {
		var (
			u      *user.User
			params url.Values

			fakeTracker *fake.Tracker
			origTracker tracker.Trackable
		)

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

			origTracker = common.Tracker
			fakeTracker = &fake.Tracker{}
			common.Tracker = fakeTracker
		})

		AfterEach(func() {
			common.Tracker = origTracker
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

			It("tracks a 'Confirmed Email' event", func() {
				doRequest()

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Confirmed Email"))
				Expect(trackCall.Arguments[2]).To(BeNil())
				Expect(trackCall.Arguments[3]).To(BeNil())
				Expect(trackCall.ReturnValues[0]).To(BeNil())
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

	Describe("GET /user", func() {
		var (
			u       *user.User
			t       *oauthtoken.OauthToken
			headers http.Header
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)

			headers = http.Header{
				"Authorization": {"Bearer " + t.Token},
			}
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/user", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		It("returns 200 OK and responds with the user's info", func() {
			doRequest()

			b := &bytes.Buffer{}
			_, err := b.ReadFrom(res.Body)
			Expect(err).To(BeNil())

			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(b.String()).To(MatchJSON(`{
				"user": {
					"email": "` + u.Email + `",
					"name": "` + u.Name + `",
					"organization": "` + u.Organization + `"
				}
			}`))
		})

		// This is also tested in the sharedexamples.ItRequiresAuthentication,
		// but we repeat it here because this endpoint is expressly for the
		// purpose of verifying access tokens.
		Context("when access token is invalid", func() {
			BeforeEach(func() {
				headers = http.Header{
					"Authorization": {"Bearer " + t.Token + "xxx"},
				}
			})

			It("returns 401 Unauthorized", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_token",
					"error_description": "access token is invalid"
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
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
					"errors": {
						"email": "is required"
					}
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
		var (
			u *user.User
			t *oauthtoken.OauthToken
		)

		BeforeEach(func() {
			u, _, t = factories.AuthTrio(db)
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
					"email":       {u.Email},
					"reset_token": {"some-reset-token"},
					"password":    {"new-password"},
				}
			})

			DescribeTable("it returns 422 and does not change the user's password",
				func(setUp func(), errorDetail string) {
					origPassword := u.Password

					setUp()
					doRequest(params)

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(422))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_params",
						` + errorDetail + `
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
				}, `"errors": {"email": "is required"}`),

				Entry("validates presence of user with given email", func() {
					params.Set("email", u.Email+"x")
				}, `"error_description": "invalid email or reset_token"`),

				Entry("requires password reset token", func() {
					params.Del("reset_token")
				}, `"errors": {"reset_token": "is required"}`),

				Entry("requires new password", func() {
					params.Del("password")
				}, `"errors": {"password": "is required"}`),
			)
		})

		Context("when the new password is in an invalid format", func() {
			It("returns 422 and does not change the user's password", func() {
				origPassword := u.Password
				newPassword := "x"

				doRequest(url.Values{
					"email":       {u.Email},
					"reset_token": {"some-reset-token"},
					"password":    {newPassword},
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
					"email":       {u.Email},
					"reset_token": {"some-reset-token"},
					"password":    {"new-password"},
				})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusForbidden))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
			    "error_description": "invalid email or reset_token"
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
					"email":       {u.Email},
					"reset_token": {u.PasswordResetToken},
					"password":    {newPassword},
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

			It("invalidates all of the user's OAuth tokens", func() {
				var tokens []*oauthtoken.OauthToken
				err := db.Where("user_id = ?", u.ID).Find(&tokens).Error
				Expect(err).To(BeNil())
				Expect(tokens).To(HaveLen(1))
				Expect(tokens[0].ID).To(Equal(t.ID))

				doRequest(url.Values{
					"email":       {u.Email},
					"reset_token": {u.PasswordResetToken},
					"password":    {"new-password"},
				})

				err = db.Where("user_id = ?", u.ID).Find(&tokens).Error
				Expect(err).To(BeNil())
				Expect(tokens).To(HaveLen(0))
			})
		})
	})
})
