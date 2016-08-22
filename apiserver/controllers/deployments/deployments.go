package deployments

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/apiserver/models/template"
	"github.com/nitrous-io/rise-server/pkg/hasher"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

const (
	viaUnknown = iota
	viaPayload
	viaCachedBundle
	viaTemplate
)

const presignExpiryDuration = 1 * time.Minute

// Create deploys a project.
func Create(c *gin.Context) {
	u := controllers.CurrentUser(c)
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	depl := &deployment.Deployment{
		ProjectID: proj.ID,
		UserID:    u.ID,
	}

	// Get js environment variables from previous deployment.
	if proj.ActiveDeploymentID != nil {
		var prevDepl deployment.Deployment
		if err := db.Where("id = ?", proj.ActiveDeploymentID).First(&prevDepl).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		depl.JsEnvVars = prevDepl.JsEnvVars
	}

	var (
		archiveFormat string
		strategy      = viaUnknown
	)

	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		strategy = viaPayload
	} else if c.PostForm("bundle_checksum") != "" {
		strategy = viaCachedBundle
	} else if c.PostForm("template_id") != "" {
		strategy = viaTemplate
	}

	switch strategy {
	case viaPayload:
		reader, err := c.Request.MultipartReader()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "the request should be encoded in multipart/form-data format",
			})
			return
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

				br := bufio.NewReader(part)
				partHead, err := br.Peek(512)
				if err != nil {
					controllers.InternalServerError(c, err)
					return
				}

				mimeType := http.DetectContentType(partHead)
				var uploadKey string
				switch mimeType {
				case "application/zip":
					uploadKey = fmt.Sprintf("deployments/%s/raw-bundle.zip", depl.PrefixID())
					archiveFormat = "zip"
				case "application/x-gzip":
					uploadKey = fmt.Sprintf("deployments/%s/raw-bundle.tar.gz", depl.PrefixID())
					archiveFormat = "tar.gz"
				default:
					c.JSON(http.StatusBadRequest, gin.H{
						"error":             "invalid_request",
						"error_description": "payload is in an unsupported format",
					})
					return
				}

				hr := hasher.NewReader(br)
				if err := s3client.Upload(uploadKey, hr, "", "private"); err != nil {
					controllers.InternalServerError(c, err)
					return
				}

				bun := &rawbundle.RawBundle{
					ProjectID:    proj.ID,
					Checksum:     hr.Checksum(),
					UploadedPath: uploadKey,
				}
				if err := db.Create(bun).Error; err != nil {
					controllers.InternalServerError(c, err)
					return
				}

				depl.RawBundleID = &bun.ID
				break
			}
		}

	case viaCachedBundle:
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

		checksum := c.PostForm("bundle_checksum")
		if checksum == "" {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"bundle_checksum": "is required",
				},
			})
			return
		}

		bun := &rawbundle.RawBundle{}
		if err := db.Where("checksum = ? AND project_id = ?", checksum, proj.ID).First(bun).Error; err != nil {
			if err == gorm.RecordNotFound {
				c.JSON(422, gin.H{
					"error": "invalid_params",
					"errors": map[string]string{
						"bundle_checksum": "the bundle could not be found",
					},
				})
				return
			}
			controllers.InternalServerError(c, err)
			return
		}
		depl.RawBundleID = &bun.ID

		// Currently bundle from CLI is always tar.gz
		archiveFormat = "tar.gz"

	case viaTemplate:
		templateID, err := strconv.ParseInt(c.PostForm("template_id"), 10, 64)
		if err != nil {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"template_id": "is invalid",
				},
			})
			return
		}

		tmpl := &template.Template{}
		if err := db.First(tmpl, templateID).Error; err != nil {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"template_id": "is not that of a known template",
				},
			})
			return
		}

		if strings.HasSuffix(tmpl.DownloadURL, ".tar.gz") {
			archiveFormat = "tar.gz"
		} else if strings.HasSuffix(tmpl.DownloadURL, ".zip") {
			archiveFormat = "zip"
		} else {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"template_id": "is no longer valid",
				},
			})
			return
		}

		ver, err := proj.NextVersion(db)
		if err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		depl.TemplateID = &tmpl.ID
		depl.Version = ver
		if err := db.Create(depl).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		bundlePath := "deployments/" + depl.PrefixID() + "/raw-bundle." + archiveFormat
		if err := s3client.Copy(tmpl.DownloadURL, bundlePath); err != nil {
			log.Printf("failed to make a copy of template %q to %q in S3, err: %v", tmpl.DownloadURL, bundlePath, err)
			controllers.InternalServerError(c, err)
			return
		}

		bun := &rawbundle.RawBundle{
			ProjectID:    proj.ID,
			UploadedPath: bundlePath,
		}
		if err := db.Create(bun).Error; err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		depl.RawBundleID = &bun.ID

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "could not understand your request",
		})
		return
	}

	if err := depl.UpdateState(db, deployment.StateUploaded); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var j *job.Job
	if proj.SkipBuild {
		j, err = job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
			DeploymentID:  depl.ID,
			UseRawBundle:  true,
			ArchiveFormat: archiveFormat,
		})
	} else {
		j, err = job.NewWithJSON(queues.Build, &messages.BuildJobData{
			DeploymentID:  depl.ID,
			ArchiveFormat: archiveFormat,
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

	newState := deployment.StatePendingBuild
	if proj.SkipBuild {
		newState = deployment.StatePendingDeploy
	}

	if err := depl.UpdateState(db, newState); err != nil {
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
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
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

// Download allows users to download an (unoptimized) tarball of the files of a
// deployment.
func Download(c *gin.Context) {
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

	if depl.RawBundleID == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "deployment cannot be downloaded",
		})
		return
	}

	bun := &rawbundle.RawBundle{}
	if err := db.First(bun, *depl.RawBundleID).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusGone, gin.H{
				"error":             "gone",
				"error_description": "deployment can no longer be downloaded",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	exists, err := s3client.Exists(bun.UploadedPath)
	if err != nil {
		log.Warnf("failed to check existence of %q on S3, err: %v", bun.UploadedPath, err)
		controllers.InternalServerError(c, err)
		return
	}
	if !exists {
		log.Warnf("deployment raw bundle %q does not exist in S3", bun.UploadedPath)
		c.JSON(http.StatusGone, gin.H{
			"error":             "gone",
			"error_description": "deployment can no longer be downloaded",
		})
		return
	}

	url, err := s3client.PresignedURL(bun.UploadedPath, presignExpiryDuration)
	if err != nil {
		log.Printf("error generating presigned URL to %q, err: %v", bun.UploadedPath, err)
		controllers.InternalServerError(c, err)
		return
	}

	c.Redirect(http.StatusFound, url)
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
		if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
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
