package common

import (
	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
)

var (
	MailerEmail = os.Getenv("MAILER_EMAIL")
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
		os.Setenv("RISE_ENV", riseEnv)
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

	if MailerEmail == "" {
		MailerEmail = "Rise.sh <support@rise.sh>"
	}

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}
}
