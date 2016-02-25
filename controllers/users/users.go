package users

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/common"
	"github.com/nitrous-io/rise-server/models/user"
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

	if err := u.Insert(); err != nil {
		c.JSON(500, gin.H{
			"error": "internal_server_error",
		})
		return
	}

	go func() {
		subject := "Please confirm your Rise account email address"

		txt := "Welcome to Rise!\n\n" +
			"To complete sign up, please confirm your email address by entering the following confirmation code when prompted by Rise CLI:\n\n" +
			u.ConfirmationCode + "\n\n" +
			"Thanks,\n" +
			"Rise"

		html := "<p>Welcome to Rise!</p>" +
			"<p>To complete sign up, please confirm your email address by entering the following confirmation code when prompted by Rise CLI:</p>" +
			"<p><strong>" + u.ConfirmationCode + "</strong></p>" +
			"<p>Thanks,<br />" +
			"Rise</p>"

		if err := common.SendMail(
			[]string{u.Email}, // tos
			nil,               // ccs
			nil,               // bccs
			subject,           // subject
			txt,               // text body
			html,              // html body
		); err != nil {
			// TODO: log error
		}
	}()

	c.JSON(http.StatusCreated, gin.H{
		"user": u.AsJSON(),
	})
}
