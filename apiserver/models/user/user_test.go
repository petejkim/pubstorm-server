package user_test

import (
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/testhelper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "user")
}

var _ = Describe("User", func() {
	var (
		u   *user.User
		db  *gorm.DB
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	Describe("Validate()", func() {
		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
		})

		It("validates email", func() {
			Expect(u.Validate()).To(BeNil())

			u.Email = ""
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["email"]).To(Equal("is required"))

			u.Email = "harry.potter@g"
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["email"]).To(Equal("is invalid"))
		})

		It("validates password", func() {
			Expect(u.Validate()).To(BeNil())

			u.Password = ""
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is required"))

			u.Password = "12345"
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is too short (min. 6 characters)"))

			u.Password = strings.Repeat("A", 73)
			Expect(u.Validate()).NotTo(BeNil())
			Expect(u.Validate()["password"]).To(Equal("is too long (max. 72 characters)"))
		})
	})

	Describe("Insert()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
			err = u.Insert(db)
			Expect(err).To(BeNil())
		})

		It("saves the record with the password encrypted", func() {
			var pwHashed bool
			db.Model(user.User{}).Where("email = ? AND encrypted_password = crypt(?, encrypted_password)", u.Email, u.Password).Count(&pwHashed)

			Expect(pwHashed).To(BeTrue())
		})

		Context("when the record already exists in the DB", func() {
			It("returns an error", func() {
				err = u.Insert(db) // attempt to save one more time
				Expect(err).To(Equal(user.ErrEmailTaken))
			})
		})
	})

	Describe("GeneratePasswordResetToken()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
			Expect(u.Insert(db)).To(BeNil())
		})

		It("generates a random password reset token and saves it", func() {
			Expect(u.PasswordResetToken).To(Equal(""))
			Expect(u.PasswordResetTokenCreatedAt).To(BeNil())

			err := u.GeneratePasswordResetToken(db)
			Expect(err).To(BeNil())

			Expect(u.PasswordResetToken).NotTo(BeEmpty())
			Expect(u.PasswordResetTokenCreatedAt).NotTo(BeNil())
		})

		It("re-generates and replaces the password reset token if the user already has one", func() {
			err := u.GeneratePasswordResetToken(db)
			Expect(err).To(BeNil())
			Expect(u.PasswordResetToken).NotTo(BeEmpty())
			Expect(u.PasswordResetTokenCreatedAt).NotTo(BeNil())

			origToken := u.PasswordResetToken
			origCreatedAt := u.PasswordResetTokenCreatedAt

			err = u.GeneratePasswordResetToken(db)
			Expect(err).To(BeNil())
			Expect(u.PasswordResetToken).NotTo(BeEmpty())
			Expect(u.PasswordResetTokenCreatedAt).NotTo(BeNil())

			Expect(u.PasswordResetToken).NotTo(Equal(origToken))
			Expect(*u.PasswordResetTokenCreatedAt).To(BeTemporally(">", *origCreatedAt))
		})
	})

	Describe("ResetPassword()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
			Expect(u.Insert(db)).To(BeNil())
		})

		It("returns an error when an empty token is used", func() {
			resetToken := ""
			err := u.ResetPassword(db, "new-password", resetToken)
			Expect(err).To(Equal(user.ErrPasswordResetTokenRequired))
		})

		It("returns an error when the user does not have a password reset token", func() {
			resetToken := "this-wont-work"
			err := u.ResetPassword(db, "new-password", resetToken)
			Expect(err).To(Equal(user.ErrPasswordResetTokenIncorrect))

			// Verify that password is unchanged.
			u2, err := user.Authenticate(db, u.Email, "123456")
			Expect(err).To(BeNil())
			Expect(u2).NotTo(BeNil())
			Expect(u2.ID).To(Equal(u.ID))
		})

		Context("when user has requested for a password reset", func() {
			BeforeEach(func() {
				err := u.GeneratePasswordResetToken(db)
				Expect(err).To(BeNil())
				Expect(u.PasswordResetToken).NotTo(BeEmpty())
			})

			It("returns an error when the token is incorrect", func() {
				resetToken := "this-wont-work"
				err := u.ResetPassword(db, "new-password", resetToken)
				Expect(err).To(Equal(user.ErrPasswordResetTokenIncorrect))

				// Verify that password is unchanged.
				u2, err := user.Authenticate(db, u.Email, "123456")
				Expect(err).To(BeNil())
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
			})

			It("sets the user's password to the new password when the token is correct", func() {
				err := u.ResetPassword(db, "new-password", u.PasswordResetToken)
				Expect(err).To(BeNil())

				// Verify that password has been changed.
				u2, err := user.Authenticate(db, u.Email, "new-password")
				Expect(err).To(BeNil())
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
			})
		})
	})

	Describe("Authenticate()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
			err = u.Insert(db)
			Expect(err).To(BeNil())
		})

		Context("when the crendentials are valid", func() {
			It("returns user", func() {
				u2, err := user.Authenticate(db, u.Email, u.Password)
				Expect(u2).NotTo(BeNil())
				Expect(u2.ID).To(Equal(u.ID))
				Expect(u2.Email).To(Equal(u.Email))
				Expect(err).To(BeNil())
			})
		})

		Context("when the crendentials are invalid", func() {
			It("returns nil", func() {
				u2, err := user.Authenticate(db, u.Email, u.Password+"x")
				Expect(u2).To(BeNil())
				Expect(err).To(BeNil())
			})
		})
	})

	Describe("Confirm()", func() {
		var u *user.User

		BeforeEach(func() {
			u = &user.User{
				Email:    "harry.potter@gmail.com",
				Password: "123456",
			}
			err = u.Insert(db)
			Expect(err).To(BeNil())
		})

		Context("when the email and confirmation code are valid", func() {
			It("returns true and confirms user", func() {
				confirmed, err := user.Confirm(db, u.Email, u.ConfirmationCode)
				Expect(confirmed).To(BeTrue())
				Expect(err).To(BeNil())

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				Expect(u.ConfirmedAt).NotTo(BeNil())
				Expect(u.ConfirmedAt.Unix()).NotTo(BeZero())
			})
		})

		Context("when the email or confirmation code is invalid", func() {
			It("returns false and does not confirm user", func() {
				confirmed, err := user.Confirm(db, u.Email, u.ConfirmationCode+"x")
				Expect(confirmed).To(BeFalse())
				Expect(err).To(BeNil())

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				Expect(u.ConfirmedAt).To(BeNil())
			})
		})

		Context("when user is already confirmed", func() {
			It("returns false and does not re-confirm user", func() {
				confirmed, err := user.Confirm(db, u.Email, u.ConfirmationCode)
				Expect(confirmed).To(BeTrue())
				Expect(err).To(BeNil())

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())
				prevConfirmedAt := u.ConfirmedAt
				Expect(prevConfirmedAt).NotTo(BeNil())

				confirmed, err = user.Confirm(db, u.Email, u.ConfirmationCode)
				Expect(confirmed).To(BeFalse())
				Expect(err).To(BeNil())

				err = db.First(u, u.ID).Error
				Expect(err).To(BeNil())

				Expect(u.ConfirmedAt).NotTo(BeNil())
				Expect(u.ConfirmedAt.Unix()).To(Equal(prevConfirmedAt.Unix()))
			})
		})
	})

	Describe("FindByEmail()", func() {
		Context("the user exists", func() {
			BeforeEach(func() {
				u = &user.User{
					Email:    "harry.potter@gmail.com",
					Password: "123456",
				}
				err = u.Insert(db)
				Expect(err).To(BeNil())
			})

			Context("when the email is valid", func() {
				It("returns user", func() {
					u1, err := user.FindByEmail(db, u.Email)
					Expect(err).To(BeNil())
					Expect(u1.ID).To(Equal(u.ID))
					Expect(u1.Email).To(Equal(u.Email))
				})
			})

			Context("the user does not exist", func() {
				It("returns nil", func() {
					u1, err := user.FindByEmail(db, u.Email+"xx")
					Expect(u1).To(BeNil())
					Expect(err).To(BeNil())
				})
			})
		})
	})
})
