package common

import (
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
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

func InternalServerError(c *gin.Context, err error) {
	var (
		errMsg  = "internal server error"
		errHash string
	)

	if err != nil {
		errMsg = err.Error()
		errHash = fmt.Sprintf("%x", sha1.Sum([]byte(errMsg)))
	}

	req := c.Request

	fields := log.Fields{
		"req": fmt.Sprintf("%s %s", req.Method, req.URL.String()),
		"ip":  c.ClientIP(),
	}

	j := gin.H{
		"error": "internal_server_error",
	}

	if errHash != "" {
		fields["hash"] = errHash
		j["error_hash"] = errHash
	}

	if (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") && strings.Contains(c.ContentType(), "application/x-www-form-urlencoded") {
		if err := req.ParseForm(); err == nil {
			fields["form"] = req.PostForm.Encode()
		}
	}

	log.WithFields(fields).Error(errMsg)
	c.JSON(http.StatusInternalServerError, j)
}
