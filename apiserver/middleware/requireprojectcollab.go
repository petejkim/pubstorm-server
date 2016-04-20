package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
)

// RequireProjectCollab is a Gin middleware that:
// 1. checks that the "project_name" parameter in the path is the name of a
//    valid project, and
// 2. ensures that the current user is the owner or a collaborator of the
//    project.
func RequireProjectCollab(c *gin.Context) {
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
	if proj == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "project could not be found",
		})
		c.Abort()
		return
	}

	if proj.UserID != u.ID {
		// If user is not the project owner, check if he is a collaborator.
		cnt := 0
		if err := db.Model(collab.Collab{}).Where("project_id = ? AND user_id = ?", proj.ID, u.ID).Count(&cnt).Error; err != nil {
			controllers.InternalServerError(c, err)
			c.Abort()
			return
		}

		if cnt == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "project could not be found",
			})
			c.Abort()
			return
		}
	}

	c.Set(controllers.CurrentProjectKey, proj)

	c.Next()
}
