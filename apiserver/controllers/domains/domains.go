package domains

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-cli-go/project"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
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

	domName := strings.ToLower(c.PostForm("name"))
	dom := &domain.Domain{
		Name:      domName,
		ProjectID: proj.ID,
	}

	dom.Sanitize()

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
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

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

	var d domain.Domain
	if err := tx.Where("name = ? AND project_id = ?", domainName, proj.ID).First(&d).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "domain could not be found",
			})
			return
		} else {
			controllers.InternalServerError(c, err)
			return
		}
	}

	metaJSONPath := "domains/" + domainName + "/meta.json"
	certificatePath := "certs/" + domainName + "/ssl.crt"
	privateKeyPath := "certs/" + domainName + "/ssl.key"
	if err := s3client.Delete(metaJSONPath, certificatePath, privateKeyPath); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := tx.Delete(d).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	q := tx.Where("domain_id = ?", d.ID).Delete(cert.Cert{})
	if q.Error != nil {
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

	if err := tx.Commit().Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
	})
}

func Update(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")
	forceHTTPS := c.PostForm("force_https") == "true"

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if domainName == proj.DefaultDomainName() {
		if !proj.DefaultDomainEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"error":             "forbidden",
				"error_description": "default domain is not enabled",
			})
			return
		}

		if proj.DefaultDomainForceHTTPS == forceHTTPS {
			c.JSON(http.StatusOK, gin.H{
				"updated": true,
			})
			return
		}

		if err := db.Model(project.Project{}).Where("id = ?", proj.ID).Update("default_domain_force_https", forceHTTPS).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}
	} else {
		d := &domain.Domain{}
		if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(&d).Error; err != nil {
			if err == gorm.RecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{
					"error":             "not_found",
					"error_description": "domain could not be found",
				})
				return
			}
			controllers.InternalServerError(c, err)
			return
		}

		if d.ForceHTTPS == forceHTTPS {
			c.JSON(http.StatusOK, gin.H{
				"updated": true,
			})
			return
		}

		if forceHTTPS {
			var count int
			if err := db.Model(cert.Cert{}).Where("domain_id = ?", d.ID).Count(&count).Error; err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			if count == 0 {
				c.JSON(http.StatusForbidden, gin.H{
					"error":             "forbidden",
					"error_description": "ssl cert could not be found for the domain",
				})
				return
			}
		}

		if err := db.Model(domain.Domain{}).Where("id = ?", d.ID).Update("force_https", forceHTTPS).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}
	}

	if proj.ActiveDeploymentID != nil {
		j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
			DeploymentID:      *proj.ActiveDeploymentID,
			SkipWebrootUpload: true,
			SkipInvalidation:  false,
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

	c.JSON(http.StatusOK, gin.H{
		"updated": true,
	})
}
