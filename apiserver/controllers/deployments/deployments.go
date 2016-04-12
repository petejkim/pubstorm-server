package deployments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

func Create(c *gin.Context) {
	u := controllers.CurrentUser(c)
	proj := controllers.CurrentProject(c)

	// get the multipart reader for the request.
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "the request should be encoded in multipart/form-data format",
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depl := &deployment.Deployment{
		ProjectID: proj.ID,
		UserID:    u.ID,
	}

	if n, err := strconv.ParseInt(c.Request.Header.Get("Content-Length"), 10, 64); err != nil || n > s3client.MaxUploadSize {
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Content-Length header is required",
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "request body is too large",
			})
		}
		return
	}

	// upload "payload" part to s3
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]interface{}{
					"payload": "is required",
				},
			})
			return
		}

		if part.FormName() == "payload" {
			if err := db.Create(depl).Error; err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			uploadKey := fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)

			if err := s3client.Upload(uploadKey, part, "", "private"); err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			break
		}
	}

	if err := db.Model(depl).Update("state", deployment.StateUploaded).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
		DeploymentID: depl.ID,
	})
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := j.Enqueue(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	// Gorm does not refetch the row from DB after update.
	// So we call `Find` again to fetch actual values particularly for time fields because of precision.
	if err := db.Model(depl).Update("state", deployment.StatePendingDeploy).Find(depl).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": depl.AsJSON(),
	})
}

func Show(c *gin.Context) {
	deploymentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(422, gin.H{
			"error":             "invalid_param",
			"error_description": "deployment_id is not a number",
		})
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depl := &deployment.Deployment{}

	if err := db.First(depl, deploymentID).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "deployment could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": depl.AsJSON(),
	})
}

func Rollback(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	if proj.ActiveDeploymentID == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "active deployment could not be found",
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	tx := db.Begin()
	if tx.Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	defer tx.Rollback()

	var currentDepl deployment.Deployment
	if err := tx.First(&currentDepl, *proj.ActiveDeploymentID).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var depl *deployment.Deployment
	if c.PostForm("deployment_id") == "" {
		depl, err = currentDepl.PreviousCompletedDeployment(db)
		if err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		if depl == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "previous completed deployment could not be found",
			})
			return
		}
	} else {
		depl = &deployment.Deployment{}

		deploymentID, err := strconv.ParseInt(c.PostForm("deployment_id"), 10, 64)
		if err != nil {
			c.JSON(422, gin.H{
				"error":             "invalid_param",
				"error_description": "deployment_id is not a number",
			})
		}

		if err := db.Where("project_id = ? AND state = ? AND id = ?", proj.ID, deployment.StateDeployed, deploymentID).First(depl).Error; err != nil {
			if err == gorm.RecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{
					"error":             "not_found",
					"error_description": "the deployment could not be found",
				})
				return
			}

			controllers.InternalServerError(c, err)
			return
		}

		if depl.ID == *proj.ActiveDeploymentID {
			c.JSON(422, gin.H{
				"error":             "invalid_request",
				"error_description": "the deployment is already active",
			})
			return
		}

		if depl.State != deployment.StateDeployed {
			c.JSON(422, gin.H{
				"error":             "invalid_request",
				"error_description": "the deployment is not in deployed state",
			})
			return
		}
	}

	if tx.Model(proj).Update(project.Project{ActiveDeploymentID: &depl.ID}).Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if tx.Raw("UPDATE deployments SET deployed_at = now() WHERE id = ? RETURNING *", depl.ID).Scan(depl).Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	domainNames, err := proj.DomainNames(tx)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	metaJson, err := json.Marshal(map[string]interface{}{
		"prefix": depl.PrefixID(),
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	reader := bytes.NewReader(metaJson)

	for _, domainName := range domainNames {
		reader.Seek(0, 0)
		if err := s3client.Upload("domains/"+domainName+"/meta.json", reader, "application/json", "public-read"); err != nil {
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

	if tx.Commit().Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": depl.AsJSON(),
	})
}

func Index(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depls, err := deployment.AllCompletedDeployments(db, proj.ID)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var deplsToJSON []interface{}
	for _, depl := range depls {
		deplJSON := depl.AsJSON()
		deplJSON.Active = depl.ID == *proj.ActiveDeploymentID
		deplsToJSON = append(deplsToJSON, deplJSON)
	}

	c.JSON(http.StatusOK, gin.H{
		"deployments": deplsToJSON,
	})
}
