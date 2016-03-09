package deployments

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/common"
	"github.com/nitrous-io/rise-server/controllers"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/deployment"
	"github.com/nitrous-io/rise-server/models/project"
)

func Create(c *gin.Context) {
	u := controllers.CurrentUser(c)
	if u == nil {
		controllers.InternalServerError(c, nil)
		return
	}

	name := c.Param("name")
	proj, err := project.FindByName(name)
	if err != nil {
		controllers.InternalServerError(c, err)
	}

	if proj == nil || proj.UserID != u.ID {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "not_found",
		})
		return
	}

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

	if n, err := strconv.ParseInt(c.Request.Header.Get("Content-Length"), 10, 64); err != nil || n > common.MaxUploadSize {
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

			key := fmt.Sprintf("%s-%d-bundle-raw.tar.gz", depl.Prefix, depl.ID)

			if err := common.Upload(key, part); err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			break
		}
	}

	if err := db.Model(&depl).UpdateColumn("state", "uploaded").Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment": depl.AsJSON(),
	})
}
