package domains

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

func Index(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	domNames, err := proj.DomainNames(db)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"domains": domNames,
	})
}

func Create(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	dom := &domain.Domain{
		Name:      c.PostForm("name"),
		ProjectID: proj.ID,
	}

	if errs := dom.Validate(); errs != nil {
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

	canCreate, err := proj.CanAddDomain(db)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if !canCreate {
		c.JSON(422, gin.H{
			"error":             "invalid_request",
			"error_description": "project cannot have more domains",
		})
		return
	}

	if err := db.Create(dom).Error; err != nil {
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

	if proj.ActiveDeploymentID != nil {
		j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
			DeploymentID:      *proj.ActiveDeploymentID,
			SkipWebrootUpload: true,
			SkipInvalidation:  true, // invalidation is not necessary because we are adding a new domain
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

	c.JSON(http.StatusCreated, gin.H{
		"domain": dom.AsJSON(),
	})
}

func Destroy(c *gin.Context) {
	domainName := c.Param("name")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := db.Where("name = ?", domainName).Delete(domain.Domain{}).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := s3client.Delete("/domains/" + domainName + "/meta.json"); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: []string{domainName},
	})
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := m.Publish(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
	})
}
