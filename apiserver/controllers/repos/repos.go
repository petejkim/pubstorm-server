package repos

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
)

func Link(c *gin.Context) {
	u := controllers.CurrentUser(c)
	proj := controllers.CurrentProject(c)

	uri, branch, secret := c.PostForm("uri"), c.PostForm("branch"), c.PostForm("secret")
	if uri == "" {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": map[string]interface{}{"uri": "is required"},
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	rp := &repo.Repo{
		ProjectID:     proj.ID,
		UserID:        u.ID,
		URI:           uri,
		Branch:        branch,
		WebhookSecret: secret,
	}
	if err := db.Create(rp).Error; err != nil {
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
			c.JSON(http.StatusConflict, gin.H{
				"error":             "already_exists",
				"error_description": "project already linked to a repository",
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"repo": rp.AsJSON(),
	})
}

func Unlink(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	tx := db.Begin()
	if err := tx.Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}
	defer tx.Rollback()

	var rp repo.Repo
	if err := tx.Where("project_id = ?", proj.ID).First(&rp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "project not linked to any repository",
		})
		return
	}

	if err := tx.Delete(rp).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := tx.Commit().Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
	})
}
