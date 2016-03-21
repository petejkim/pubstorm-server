package users

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
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

	confirmed, err := user.Confirm(c.PostForm("email"), c.PostForm("confirmation_code"))
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

	u, err := user.FindByEmail(email)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if u == nil || u.ConfirmedAt.Valid {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is not found or already confirmed",
			"sent":              false,
		})
		return
	}

	if err = sendConfirmationEmail(u); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sent": true,
	})
}

func sendConfirmationEmail(u *user.User) error {
	subject := "Please confirm your Rise account email address"

	txt := "Welcome to Rise!\n\n" +
		"To complete sign up, please confirm your email address by entering the following confirmation code when logging in for the first time:\n\n" +
		u.ConfirmationCode + "\n\n" +
		"Thanks,\n" +
		"Rise"

	html := "<p>Welcome to Rise!</p>" +
		"<p>To complete sign up, please confirm your email address by entering the following confirmation code when logging in for the first time:</p>" +
		"<p><strong>" + u.ConfirmationCode + "</strong></p>" +
		"<p>Thanks,<br />" +
		"Rise</p>"

	return common.SendMail(
		[]string{u.Email}, // tos
		nil,               // ccs
		nil,               // bccs
		subject,           // subject
		txt,               // text body
		html,              // html body
	)
}
