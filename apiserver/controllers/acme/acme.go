package acme

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
)

func ChallengeResponse(c *gin.Context) {
	token := c.Param("token")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	// The Let's Encrypt challenge path is always of this form.
	challengePath := "/.well-known/acme-challenge/" + token

	acmeCert := &acmecert.AcmeCert{}
	if err := db.Where("http_challenge_path = ?", challengePath).First(acmeCert).Error; err != nil {
		c.String(http.StatusNotFound, "")
		return
	}

	c.String(http.StatusOK, acmeCert.HTTPChallengeResource)
}
