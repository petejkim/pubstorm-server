package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
)

func LockProject(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	if proj == nil {
		controllers.InternalServerError(c, nil)
		c.Abort()
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		c.Abort()
		return
	}

	acquired, err := proj.Lock(db)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if acquired {
		defer func() {
			if err := proj.Unlock(db); err != nil {
				controllers.InternalServerError(c, err)
				return
			}
		}()
	} else {
		c.JSON(423, gin.H{
			"error":             "locked",
			"error_description": "project is locked",
		})
		c.Abort()
		return
	}

	c.Next()
}
