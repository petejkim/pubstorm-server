package projects

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
)

func ListCollaborators(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	collaborators := []struct {
		Email string `json:"email"`
	}{}

	if err := db.Model(collab.Collab{}).Select("users.email").Joins("JOIN projects ON projects.id = collabs.project_id JOIN users ON users.id = collabs.user_id").Where("collabs.project_id = ?", proj.ID).Order("users.email ASC").Scan(&collaborators).Error; err != nil {
		fmt.Println(err)
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"collaborators": collaborators,
	})
}

func AddCollaborator(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.FindByEmail(db, c.PostForm("email"))
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	if u == nil {
		// TODO Support adding collaborator who does not already have an account.
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "email is not found",
		})
		return
	}

	if err := proj.AddCollaborator(db, u); err != nil {
		switch err {
		case project.ErrCollaboratorIsOwner:
			c.JSON(422, gin.H{
				"error":             "invalid_request",
				"error_description": "the owner of a project cannot be added as a collaborator",
			})
		case project.ErrCollaboratorAlreadyExists:
			c.JSON(http.StatusConflict, gin.H{
				"error":             "already_exists",
				"error_description": "user is already a collaborator",
			})
		default:
			controllers.InternalServerError(c, err)
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"added": true,
	})
}

func RemoveCollaborator(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	u, err := user.FindByEmail(db, c.Param("email"))
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	if u == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "email is not found",
		})
		return
	}

	if err := proj.RemoveCollaborator(db, u); err != nil {
		// Ignore if user is not already a collaborator - we respond as if user
		// was removed.
		if err != project.ErrNotCollaborator {
			controllers.InternalServerError(c, err)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"removed": true,
	})
}
