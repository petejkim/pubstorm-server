package oauth

import (
	"encoding/base64"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthclient"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
)

func CreateToken(c *gin.Context) {
	for _, p := range []string{"grant_type", "username", "password"} {
		if c.PostForm(p) == "" {
			c.JSON(400, gin.H{
				"error":             "invalid_request",
				"error_description": `"` + p + `" is required`,
			})
			return
		}
	}

	grantType := c.PostForm("grant_type")
	email := c.PostForm("username") // OAuth 2 spec requires this to be called "username"
	password := c.PostForm("password")

	if grantType != "password" {
		c.JSON(400, gin.H{
			"error":             "unsupported_grant_type",
			"error_description": `grant type "` + grantType + `" is not supported`,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.Authenticate(db, email, password)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if u == nil {
		c.JSON(400, gin.H{
			"error":             "invalid_grant",
			"error_description": "user credentials are invalid",
		})
		return
	}

	if u.ConfirmedAt == nil {
		c.JSON(400, gin.H{
			"error":             "invalid_grant",
			"error_description": "user has not confirmed email address",
		})
		return
	}

	var clientID, clientSecret string

	authHeader := strings.TrimPrefix(c.Request.Header.Get("Authorization"), "Basic ")
	if authHeader != "" {
		authBytes, err := base64.StdEncoding.DecodeString(authHeader)
		if err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		authPair := strings.SplitN(string(authBytes), ":", 2)
		clientID = authPair[0]
		clientSecret = authPair[1]
	} else {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}

	client, err := oauthclient.Authenticate(db, clientID, clientSecret)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if client == nil {
		c.Header("WWW-Authenticate", `Basic realm="rise-oauth-client"`)
		c.JSON(401, gin.H{
			"error":             "invalid_client",
			"error_description": "client credentials are invalid",
		})
		return
	}

	token := &oauthtoken.OauthToken{
		UserID:        u.ID,
		OauthClientID: client.ID,
	}
	if err := db.Create(token).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		var (
			event = "User Logged In"
			props = map[string]interface{}{
				"oauthClientId":   client.ID,
				"oauthClientName": client.Name,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(200, gin.H{
		"access_token": token.Token,
		"token_type":   "bearer",
		"client_id":    client.ClientID,
	})
}

func DestroyToken(c *gin.Context) {
	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	t := controllers.CurrentToken(c)
	if t == nil {
		controllers.InternalServerError(c, err)
		return
	}

	delQuery := db.Where("token = ?", t.Token).Delete(oauthtoken.OauthToken{})
	if err := delQuery.Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		u := controllers.CurrentUser(c)

		var (
			event          = "User Logged Out"
			props, context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(200, gin.H{
		"invalidated": true,
	})
}
