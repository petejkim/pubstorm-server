package users

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/user"
)

func Create(c *gin.Context) {
	db, err := dbconn.DB()
	if err != nil {
		c.JSON(500, gin.H{
			"error": "internal_server_error",
		})
		return
	}

	u := &user.User{
		Email:    c.PostForm("email"),
		Password: c.PostForm("password"),
	}

	if errs := u.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"errors": errs,
		})
		return
	}

	db.Table("users").Raw(`INSERT INTO users (
		email,
		encrypted_password
	) VALUES (
		?,
		crypt(?, gen_salt('bf'))
	) RETURNING *;`, u.Email, u.Password).Scan(u)

	c.JSON(http.StatusCreated, gin.H{
		"user": u.AsJSON(),
	})
}
