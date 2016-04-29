package deployments

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/pkg/hasher"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

// Create deploys a project.
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
			ver, err := proj.NextVersion(db)
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			depl.Version = ver
			if err := db.Create(depl).Error; err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			hashReader := hasher.NewReader(part)
			uploadKey := fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)
			if err := s3client.Upload(uploadKey, hashReader, "", "private"); err != nil {
				controllers.InternalServerError(c, err)
				return
			}

			bun := &rawbundle.RawBundle{}
			bun.ProjectID = proj.ID
			bun.Checksum = hashReader.Checksum()
			bun.UploadedPath = uploadKey
			if err := db.Create(bun).Error; err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			depl.RawBundleID = &bun.ID

			break
		}
	}

	if err = depl.UpdateState(db, deployment.StateUploaded); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var j *job.Job
	if proj.SkipBuild {
		j, err = job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
			DeploymentID: depl.ID,
			UseRawBundle: true,
		})
	} else {
		j, err = job.NewWithJSON(queues.Build, &messages.BuildJobData{
			DeploymentID: depl.ID,
		})
	}

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := j.Enqueue(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	nextState := deployment.StatePendingBuild
	if proj.SkipBuild {
		nextState = deployment.StatePendingDeploy
	}

	if err := depl.UpdateState(db, nextState); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	{
		var (
			event = "Initiated Project Deployment"
			props = map[string]interface{}{
				"projectName":       proj.Name,
				"deploymentId":      depl.ID,
				"deploymentPrefix":  depl.Prefix,
				"deploymentVersion": depl.Version,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": depl.AsJSON(),
	})
}

// Show displays information of a single deployment.
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

// Rollback either rolls back a project to the previous deployment, or to a
// given version.
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

	var currentDepl deployment.Deployment
	if err := db.First(&currentDepl, *proj.ActiveDeploymentID).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var depl *deployment.Deployment
	if c.PostForm("version") == "" {
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
		version, err := strconv.ParseInt(c.PostForm("version"), 10, 64)
		if err != nil {
			c.JSON(422, gin.H{
				"error":  "invalid_params",
				"errors": map[string]string{"version": "is not a number"},
			})
			return
		}

		if err := db.Where("project_id = ? AND state = ? AND version = ?", proj.ID, deployment.StateDeployed, version).First(depl).Error; err != nil {
			if err == gorm.RecordNotFound {
				c.JSON(422, gin.H{
					"error":             "invalid_request",
					"error_description": "completed deployment with a given version could not be found",
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

	{
		u := controllers.CurrentUser(c)

		var (
			event = "Initiated Project Rollback"
			props = map[string]interface{}{
				"projectName":     proj.Name,
				"deployedVersion": currentDepl.Version,
				"targetVersion":   depl.Version,
			}
			context map[string]interface{}
		)
		if err := common.Track(strconv.Itoa(int(u.ID)), event, props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, u.ID, err)
		}
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": depl.AsJSON(),
	})
}

// Index lists all deployments of a project.
func Index(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depls, err := deployment.CompletedDeployments(db, proj.ID, proj.MaxDeploysKept)
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
