package common

import (
	"io/ioutil"
	"os"

	"github.com/nitrous-io/rise-server/pkg/mailer"

	log "github.com/Sirupsen/logrus"
)

var MailerEmail = os.Getenv("MAILER_EMAIL")

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
	}

	if MailerEmail == "" {
		MailerEmail = "Rise.sh <support@rise.sh>"
	}

	logLevelEnv := os.Getenv("RISE_LOG_LEVEL")
	logLevel, err := log.ParseLevel(logLevelEnv)
	if err != nil {
		switch riseEnv {
		case "production":
			log.SetLevel(log.InfoLevel)
			log.SetOutput(os.Stderr)
		case "test":
			log.SetLevel(log.PanicLevel)
			log.SetOutput(ioutil.Discard)
		default:
			log.SetLevel(log.DebugLevel)
			log.SetOutput(os.Stderr)
		}
	} else {
		log.SetLevel(logLevel)
	}
}

var Mailer mailer.Mailer = mailer.NewSendGridMailer(os.Getenv("SENDGRID_USERNAME"), os.Getenv("SENDGRID_PASSWORD"))

func SendMail(tos, ccs, bccs []string, subject, body, htmltext string) error {
	return Mailer.SendMail(MailerEmail, tos, ccs, bccs, MailerEmail, subject, body, htmltext)
}
