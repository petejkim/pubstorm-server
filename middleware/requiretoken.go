package middleware

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/controllers"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/user"
)

var bearerTokenAuthHeaderRe = regexp.MustCompile(`\A\s*Bearer\s+([\S]+)\s*\z`)

func RequireToken(c *gin.Context) {
	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		c.Abort()
		return
	}

	authHeader := c.Request.Header.Get("Authorization")
	match := bearerTokenAuthHeaderRe.FindStringSubmatch(authHeader)
	if match == nil || len(match) < 1 {
		c.Header("WWW-Authenticate", `Bearer realm="rise-user"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "access token is required",
		})
		c.Abort()
		return
	}

	t, err := oauthtoken.FindByToken(match[1])
	if err != nil {
		controllers.InternalServerError(c, err)
		c.Abort()
		return
	}

	if t == nil {
		c.Header("WWW-Authenticate", `Bearer realm="rise-user"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "access token is invalid",
		})
		c.Abort()
		return
	}

	u := &user.User{}

	if err := db.Model(&t).Related(&u).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.Header("WWW-Authenticate", `Bearer realm="rise-user"`)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":             "invalid_token",
				"error_description": "access token is invalid",
			})
		} else {
			controllers.InternalServerError(c, err)
		}
		c.Abort()
		return
	}

	c.Set(controllers.CurrentTokenKey, t)
	c.Set(controllers.CurrentUserKey, u)

	c.Next()
}
