package common

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/pkg/mailer"
)

var MailerEmail = os.Getenv("MAILER_EMAIL")

func init() {
	if MailerEmail == "" {
		MailerEmail = "Rise.sh <support@rise.sh>"
	}
}

var Mailer mailer.Mailer = mailer.NewSendGridMailer(os.Getenv("SENDGRID_USERNAME"), os.Getenv("SENDGRID_PASSWORD"))

func SendMail(tos, ccs, bccs []string, subject, body, htmltext string) error {
	return Mailer.SendMail(MailerEmail, tos, ccs, bccs, MailerEmail, subject, body, htmltext)
}

func InternalServerError(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": "internal_server_error",
	})
}
