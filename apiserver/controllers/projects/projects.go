package projects

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/disallowed_project_name"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
)

func Create(c *gin.Context) {
	u := controllers.CurrentUser(c)

	proj := &project.Project{
		Name:   c.PostForm("name"),
		UserID: u.ID,
	}

	if errs := proj.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	blacklisted, err := disallowed_project_name.IsBlacklisted(db, proj.Name)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if blacklisted {
		c.JSON(422, gin.H{
			"error": "invalid_params",
			"errors": map[string]interface{}{
				"name": "is not allowed",
			},
		})
		return
	}

	if err := db.Create(proj).Error; err != nil {
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]interface{}{
					"name": "is taken",
				},
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"project": proj.AsJSON(),
	})
}

func Get(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	c.JSON(http.StatusOK, gin.H{
		"project": proj.AsJSON(),
	})
}
