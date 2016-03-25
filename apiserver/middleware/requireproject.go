package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
)

func RequireProject(c *gin.Context) {
	u := controllers.CurrentUser(c)
	if u == nil {
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

	name := c.Param("project_name")
	proj, err := project.FindByName(db, name)
	if err != nil {
		controllers.InternalServerError(c, err)
		c.Abort()
		return
	}

	if proj == nil || proj.UserID != u.ID {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "project could not be found",
		})
		c.Abort()
		return
	}

	c.Set(controllers.CurrentProjectKey, proj)

	c.Next()
}
