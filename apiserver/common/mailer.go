package common

import (
	"os"

	"github.com/nitrous-io/rise-server/pkg/mailer"
)

var (
	Mailer mailer.Mailer = mailer.NewSendGridMailer(os.Getenv("SENDGRID_USERNAME"), os.Getenv("SENDGRID_PASSWORD"))
)

func SendMail(tos, ccs, bccs []string, subject, body, htmltext string) error {
	return Mailer.SendMail(MailerEmail, tos, ccs, bccs, MailerEmail, subject, body, htmltext)
}
