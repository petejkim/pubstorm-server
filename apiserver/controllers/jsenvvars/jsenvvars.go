package jsenvvars

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
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

	newDepl, err := deployWithJsEnvVars(db, u, proj, &depl, &currentJsEnvVars)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": newDepl.AsJSON(),
	})
}

func Delete(c *gin.Context) {
	u := controllers.CurrentUser(c)
	proj := controllers.CurrentProject(c)

	if proj.ActiveDeploymentID == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error":             "precondition_failed",
			"error_description": "current active deployment could not be found",
		})
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	keys := c.Request.PostForm["keys"]
	if keys == nil || len(keys) == 0 {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "request body is empty",
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
	for _, key := range keys {
		if _, ok := currentJsEnvVars[key]; ok {
			delete(currentJsEnvVars, key)
			n += 1
		}
	}

	if n == 0 {
		c.JSON(http.StatusAccepted, gin.H{
			"deployment": depl.AsJSON(),
		})
		return
	}

	newDepl, err := deployWithJsEnvVars(db, u, proj, &depl, &currentJsEnvVars)
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": newDepl.AsJSON(),
	})
}

func Index(c *gin.Context) {
	proj := controllers.CurrentProject(c)

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

	var jsEnvVars map[string]string
	if err := json.Unmarshal(depl.JsEnvVars, &jsEnvVars); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"js_env_vars": jsEnvVars,
	})
	return
}

func deployWithJsEnvVars(db *gorm.DB, u *user.User, proj *project.Project, currentDepl *deployment.Deployment, jsEnvVars *map[string]string) (*deployment.Deployment, error) {
	updatedJSON, err := json.Marshal(&jsEnvVars)
	if err != nil {
		return nil, err
	}

	newDepl := &deployment.Deployment{
		ProjectID:   proj.ID,
		UserID:      u.ID,
		JsEnvVars:   updatedJSON,
		RawBundleID: currentDepl.RawBundleID,
	}

	ver, err := proj.NextVersion(db)
	if err != nil {
		return nil, err
	}

	newDepl.Version = ver
	if err := db.Create(newDepl).Error; err != nil {
		return nil, err
	}

	j, err := job.NewWithJSON(queues.Build, &messages.BuildJobData{DeploymentID: newDepl.ID})
	if err != nil {
		return nil, err
	}

	if err := j.Enqueue(); err != nil {
		return nil, err
	}

	if err := newDepl.UpdateState(db, deployment.StatePendingBuild); err != nil {
		return nil, err
	}

	return newDepl, nil
}
