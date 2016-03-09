package common

import (
	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
)

var (
	MailerEmail    = os.Getenv("MAILER_EMAIL")
	S3BucketRegion = os.Getenv("S3_BUCKET_REGION")
	S3BucketName   = os.Getenv("S3_BUCKET_NAME")

	S3PartSize = int64(20 * 1024 * 1024) // 20 MiB
	S3MaxParts = 5

	MaxUploadSize = int64(1024 * 1024 * 100) // 100 MiB
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
	}

	if MailerEmail == "" {
		MailerEmail = "Rise.sh <support@rise.sh>"
	}

	if S3BucketRegion == "" {
		S3BucketRegion = "us-west-2"
	}

	if S3BucketName == "" {
		S3BucketName = "rise-development-usw2"
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

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}
}
