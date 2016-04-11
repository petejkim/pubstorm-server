package users

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
)

func Create(c *gin.Context) {
	u := &user.User{
		Email:    c.PostForm("email"),
		Password: c.PostForm("password"),
	}

	if errs := u.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	tx := db.Begin()
	if err := tx.Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	defer tx.Rollback()

	if err := u.Insert(tx); err != nil {
		if err == user.ErrEmailTaken {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"email": "is taken",
				},
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	if err := sendConfirmationEmail(u); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := tx.Commit().Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": u.AsJSON(),
	})
}

func Confirm(c *gin.Context) {
	for _, k := range []string{"email", "confirmation_code"} {
		if c.PostForm(k) == "" {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": k + " is required",
				"confirmed":         false,
			})
			return
		}
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	confirmed, err := user.Confirm(db, c.PostForm("email"), c.PostForm("confirmation_code"))
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if !confirmed {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "invalid email or confirmation_code",
			"confirmed":         false,
		})
		return
	}

	c.JSON(200, gin.H{
		"confirmed": true,
	})
}

func ResendConfirmationCode(c *gin.Context) {
	email := c.PostForm("email")
	if email == "" {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is required",
			"sent":              false,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.FindByEmail(db, email)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if u == nil || u.ConfirmedAt != nil {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is not found or already confirmed",
			"sent":              false,
		})
		return
	}

	if err := sendConfirmationEmail(u); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sent": true,
	})
}

// ForgotPassword allows users who forgot their password to request for a token
// that will allow them to reset their password (see the ResetPassword handler).
// The token will be sent to their email address to verify their identity.
func ForgotPassword(c *gin.Context) {
	email := c.PostForm("email")
	if email == "" {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is required",
			"sent":              false,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.FindByEmail(db, email)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	if u == nil {
		// If no user with the given email exists, pretend that it does.
		// This is to prevent abusers from figuring out what email addresses
		// are valid.
		c.JSON(http.StatusOK, gin.H{
			"sent": true,
		})
		return
	}

	if err := u.GeneratePasswordResetToken(db); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := sendPasswordResetToken(u); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sent": true,
	})
}

func ResetPassword(c *gin.Context) {
	for _, k := range []string{"email", "reset_token", "new_password"} {
		if c.PostForm(k) == "" {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": k + " is required",
				"reset":             false,
			})
			return
		}
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.FindByEmail(db, c.PostForm("email"))
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	if u == nil {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is not found",
			"reset":             false,
		})
		return
	}

	newPassword := c.PostForm("new_password")
	u.Password = newPassword
	if errs := u.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
			"reset":  false,
		})
		return
	}

	if err := u.ResetPassword(db, newPassword, c.PostForm("reset_token")); err != nil {
		if err == user.ErrPasswordResetTokenIncorrect {
			c.JSON(http.StatusForbidden, gin.H{
				"error":             "invalid_params",
				"error_description": "reset_token is incorrect",
				"reset":             false,
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	// Invalidate all tokens for the user for security - user should be required
	// to login with their new password.
	delQuery := db.Where("user_id = ?", u.ID).Delete(oauthtoken.OauthToken{})
	if delQuery.Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reset": true,
	})
}

func sendConfirmationEmail(u *user.User) error {
	subject := "Please confirm your PubStorm account email address"

	txt := "Welcome to PubStorm!\n\n" +
		"To complete sign up, please confirm your email address by entering the following confirmation code when logging in for the first time:\n\n" +
		u.ConfirmationCode + "\n\n" +
		"Thanks,\n" +
		"PubStorm"

	html := "<p>Welcome to PubStorm!</p>" +
		"<p>To complete sign up, please confirm your email address by entering the following confirmation code when logging in for the first time:</p>" +
		"<p><strong>" + u.ConfirmationCode + "</strong></p>" +
		"<p>Thanks,<br />" +
		"PubStorm</p>"

	return common.SendMail(
		[]string{u.Email}, // tos
		nil,               // ccs
		nil,               // bccs
		subject,           // subject
		txt,               // text body
		html,              // html body
	)
}

func sendPasswordResetToken(u *user.User) error {
	subject := "PubStorm password reset instructions"

	txt := "Someone (hopefully you!) requested a password reset for your PubStorm account.\n\n" +
		"To reset your password, please use the following code with the PubStorm CLI:\n\n" +
		u.PasswordResetToken + "\n\n" +
		"You can use `storm password.reset --continue` to enter this code." + "\n\n" +
		"Thanks,\n" +
		"PubStorm"

	html := "<p>Someone (hopefully you!) requested a password reset for your PubStorm account.</p>" +
		"<p>To reset your password, please use the following code with the PubStorm CLI:</p>" +
		"<p><strong>" + u.PasswordResetToken + "</strong></p>" +
		"<p>You can use <code>storm password.reset --continue</code> to enter this code.</p>" +
		"<p>Thanks,<br />" +
		"PubStorm</p>"

	return common.SendMail(
		[]string{u.Email}, // tos
		nil,               // ccs
		nil,               // bccs
		subject,           // subject
		txt,               // text body
		html,              // html body
	)
}
