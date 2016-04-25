package jsenvvars

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

func Add(c *gin.Context) {
	u := controllers.CurrentUser(c)
	proj := controllers.CurrentProject(c)

	var newJSEnvVars map[string]string
	if err := c.Bind(&newJSEnvVars); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "request body is in invalid format",
		})
		return
	}

	if len(newJSEnvVars) == 0 {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "request body is empty",
		})
		return
	}

	if proj.ActiveDeploymentID == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error":             "precondition_failed",
			"error_description": "current active deployment could not be found",
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var depl deployment.Deployment
	if err := db.First(&depl, *proj.ActiveDeploymentID).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var currentJsEnvVars map[string]string
	if err := json.Unmarshal(depl.JsEnvVars, &currentJsEnvVars); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var n int
	for key, value := range newJSEnvVars {
		if currentJsEnvVars[key] != value {
			currentJsEnvVars[key] = value
			n += 1
		}
	}

	if n == 0 {
		c.JSON(http.StatusAccepted, gin.H{
			"deployment": depl.AsJSON(),
		})
		return
	}

	updatedJSON, err := json.Marshal(&currentJsEnvVars)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	newDepl := &deployment.Deployment{
		ProjectID: proj.ID,
		UserID:    u.ID,
		JsEnvVars: updatedJSON,
	}

	ver, err := proj.NextVersion(db)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	newDepl.Version = ver
	if err := db.Create(newDepl).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	currentUploadKey := fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", depl.Prefix, depl.ID)
	newUploadKey := fmt.Sprintf("deployments/%s-%d/raw-bundle.tar.gz", newDepl.Prefix, newDepl.ID)
	if err := s3client.Copy(currentUploadKey, newUploadKey); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{DeploymentID: newDepl.ID})
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := j.Enqueue(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := newDepl.UpdateState(db, deployment.StatePendingDeploy); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": newDepl.AsJSON(),
	})
}
