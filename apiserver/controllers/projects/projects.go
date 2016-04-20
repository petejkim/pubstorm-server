package projects

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedname"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
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

func Update(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	// Make a copy of the original project.
	updatedProj := *proj

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if n := c.PostForm("name"); n != "" {
		updatedProj.Name = n

		blacklisted, err := blacklistedname.IsBlacklisted(db, updatedProj.Name)
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
	}

	if c.PostForm("default_domain_enabled") != "" {
		defaultDomainEnabled, _ := strconv.ParseBool(c.PostForm("default_domain_enabled"))
		updatedProj.DefaultDomainEnabled = defaultDomainEnabled
	}

	if errs := updatedProj.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
		})
		return
	}

	// If there is an active deployment, activate/deactivate default domain.
	if proj.ActiveDeploymentID != nil {
		// If default domain was just enabled, we need to add it so that it'll
		// actually work.
		activate := !proj.DefaultDomainEnabled && updatedProj.DefaultDomainEnabled
		if activate {
			j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
				DeploymentID:      *proj.ActiveDeploymentID,
				SkipWebrootUpload: true,
				SkipInvalidation:  true,
			})
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			if err := j.Enqueue(); err != nil {
				controllers.InternalServerError(c, err)
				return
			}
		}

		// If default domain was just disabled, we need to remove it so that it
		// no longer works.
		deactivate := proj.DefaultDomainEnabled && !updatedProj.DefaultDomainEnabled
		if deactivate {
			defaultDomain := proj.Name + "." + shared.DefaultDomain

			if err := s3client.Delete("/domains/" + defaultDomain + "/meta.json"); err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
				Domains: []string{defaultDomain},
			})
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			if err := m.Publish(); err != nil {
				controllers.InternalServerError(c, err)
				return
			}
		}
	}

	if err := db.Save(&updatedProj).Error; err != nil {
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

	// FIXME If project was renamed, we need to handle the change in default domain

	c.JSON(http.StatusOK, gin.H{
		"project": updatedProj.AsJSON(),
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
