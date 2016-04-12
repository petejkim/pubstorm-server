package deployments

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/pkg/job"
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

	if err := depl.UpdateState(db, deployment.StateUploaded); err != nil {
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
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "deployment could not be found",
		})
		return
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
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error":             "precondition_failed",
			"error_description": "active deployment could not be found",
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var depl *deployment.Deployment
	if c.PostForm("deployment_id") == "" {
		var currentDepl deployment.Deployment
		if err := db.First(&currentDepl, *proj.ActiveDeploymentID).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		depl, err = currentDepl.PreviousCompletedDeployment(db)
		if err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		if depl == nil {
			c.JSON(http.StatusPreconditionFailed, gin.H{
				"error":             "precondition_failed",
				"error_description": "previous completed deployment could not be found",
			})
			return
		}
	} else {
		depl = &deployment.Deployment{}
		deploymentID, err := strconv.ParseInt(c.PostForm("deployment_id"), 10, 64)
		if err != nil {
			c.JSON(422, gin.H{
				"error":  "invalid_params",
				"errors": map[string]string{"deployment_id": "is not a number"},
			})
			return
		}

		if err := db.Where("project_id = ? AND state = ? AND id = ?", proj.ID, deployment.StateDeployed, deploymentID).First(depl).Error; err != nil {
			if err == gorm.RecordNotFound {
				c.JSON(422, gin.H{
					"error":             "invalid_request",
					"error_description": "complete deployment with given id could not be found",
				})
				return
			}

			controllers.InternalServerError(c, err)
			return
		}

		if depl.ID == *proj.ActiveDeploymentID {
			c.JSON(422, gin.H{
				"error":             "invalid_request",
				"error_description": "the specified deployment is already active",
			})
			return
		}
	}

	j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
		DeploymentID:      depl.ID,
		SkipWebrootUpload: true,
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := j.Enqueue(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := depl.UpdateState(db, deployment.StatePendingRollback); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
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
