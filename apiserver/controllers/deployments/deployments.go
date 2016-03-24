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

	if err := db.Model(depl).Update("state", deployment.StatePendingDeploy).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": depl.AsJSON(),
	})
}

func Show(c *gin.Context) {
	deploymentID := c.Param("id")
	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depl := &deployment.Deployment{}
	if err := db.Where("id = ?", deploymentID).First(depl).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":         "not_found",
				"error_message": "deployment could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": depl.AsJSON(),
	})
	return
}
