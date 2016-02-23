package users

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/models/user"
)

func Create(c *gin.Context) {
	u := &user.User{
		Email:    c.PostForm("email"),
		Password: c.PostForm("password"),
	}

	if errs := u.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
		})
		return
	}

	if err := u.Insert(); err != nil {
		c.JSON(500, gin.H{
			"error": "internal_server_error",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": u.AsJSON(),
	})
}
