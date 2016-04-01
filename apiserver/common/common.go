package common

import (
	"io/ioutil"
	"os"
	"strconv"

	log "github.com/Sirupsen/logrus"
)

var (
	MailerEmail   = os.Getenv("MAILER_EMAIL")
	DefaultDomain = os.Getenv("DEFAULT_DOMAIN") // default domain (e.g. rise.cloud)

	MaxDomainsPerProject = 5 // MAX_DOMAINS - max # of custom domains per project
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
		MailerEmail = "PubStorm <support@pubstorm.com>"
	}

	if DefaultDomain == "" {
		DefaultDomain = "pubstorm.site"
	}

	if maxDomainsEnv := os.Getenv("MAX_DOMAINS"); maxDomainsEnv != "" {
		n, err := strconv.Atoi(maxDomainsEnv)
		if err != nil {
			log.Warn("Ignoring MAX_DOMAINS, not a valid numeric value!")
		} else {
			MaxDomainsPerProject = n
		}
	}

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}
}
