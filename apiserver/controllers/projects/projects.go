package projects

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/blacklistedname"
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

	projName := strings.ToLower(c.PostForm("name"))
	proj := &project.Project{
		Name:   projName,
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
	if err := db.Order("name ASC").
		Where("user_id = ?", u.ID).
		Find(&projects).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	projectsAsJson := []interface{}{}
	for _, proj := range projects {
		projectsAsJson = append(projectsAsJson, proj.AsJSON())
	}

	sharedProjects := []*project.Project{}
	if err := db.Order("projects.name ASC").
		Joins("JOIN users ON users.id = collabs.user_id").
		Joins("JOIN collabs ON collabs.project_id = projects.id").
		Where("collabs.user_id = ?", u.ID).
		Find(&sharedProjects).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	sharedProjectsAsJson := []interface{}{}
	for _, proj := range sharedProjects {
		sharedProjectsAsJson = append(sharedProjectsAsJson, proj.AsJSON())
	}

	c.JSON(http.StatusOK, gin.H{
		"projects":        projectsAsJson,
		"shared_projects": sharedProjectsAsJson,
	})
}

func Update(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	// Make a copy of the original project.
	updatedProj := *proj

	if c.PostForm("default_domain_enabled") != "" {
		defaultDomainEnabled, _ := strconv.ParseBool(c.PostForm("default_domain_enabled"))
		if defaultDomainEnabled == proj.DefaultDomainEnabled {
			c.JSON(http.StatusOK, gin.H{
				"project": proj.AsJSON(),
			})
			return
		}
		updatedProj.DefaultDomainEnabled = defaultDomainEnabled
	}

	// If default domain was just enabled, we need add it so that it'll actually
	// work.
	activate := !proj.DefaultDomainEnabled &&
		updatedProj.DefaultDomainEnabled &&
		proj.ActiveDeploymentID != nil
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

	// If default domain was just disabled, we need to remove it so that it no
	// longer works.
	deactivate := proj.DefaultDomainEnabled &&
		!updatedProj.DefaultDomainEnabled &&
		proj.ActiveDeploymentID != nil
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

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := db.Save(&updatedProj).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

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

	// Delete ssl certs from S3
	var filesToDelete []string
	for _, domainName := range domainNames {
		filesToDelete = append(filesToDelete, "domains/"+domainName+"/meta.json")
		if domainName != proj.DefaultDomainName() {
			filesToDelete = append(filesToDelete, "certs/"+domainName+"/ssl.crt")
			filesToDelete = append(filesToDelete, "certs/"+domainName+"/ssl.key")
		}
	}

	if err := s3client.Delete(filesToDelete...); err != nil {
		controllers.InternalServerError(c, err)
		return
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

	if err := proj.Destroy(tx); err != nil {
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
