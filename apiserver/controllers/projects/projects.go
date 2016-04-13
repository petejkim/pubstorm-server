package projects

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedname"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/s3client"
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

	blacklisted, err := blacklistedname.IsBlacklisted(db, proj.Name)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if blacklisted {
		c.JSON(422, gin.H{
			"error": "invalid_params",
			"errors": map[string]interface{}{
				"name": "is taken",
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

func Index(c *gin.Context) {
	u := controllers.CurrentUser(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	projects := []*project.Project{}
	if err := db.Order("name ASC").Where("user_id = ?", u.ID).Find(&projects).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var projectsAsJson []interface{}
	for _, proj := range projects {
		projectsAsJson = append(projectsAsJson, proj.AsJSON())
	}

	c.JSON(http.StatusOK, gin.H{
		"projects": projectsAsJson,
	})
}

func Destroy(c *gin.Context) {
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

	domainNames, err := proj.DomainNames(db)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	for _, domainName := range domainNames {
		if err := s3client.Delete("/domains/" + domainName + "/meta.json"); err != nil {
			controllers.InternalServerError(c, err)
			return
		}
	}

	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: domainNames,
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := m.Publish(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := tx.Delete(domain.Domain{}, "project_id = ?", proj.ID).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := tx.Delete(proj).Error; err != nil {
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
