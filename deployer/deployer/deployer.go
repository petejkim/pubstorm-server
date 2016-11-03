package deployer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/mimetypes"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

var (
	ErrProjectLocked   = errors.New("project is locked")
	ErrRecordNotFound  = errors.New("project or deployment is deleted")
	ErrTimeout         = errors.New("failed to upload files due to timeout on uploading to s3")
	ErrUnarchiveFailed = errors.New("Failed to unarchive file")

	MaxFileSizeToWatermark int64 = 5 * 1000 * 1000 // in bytes
	UploadTimeout                = 3 * time.Minute
)

var jsenvFormat = `(function(global, env) {
	if (typeof module === "object" && typeof module.exports === "object") {
		module.exports = env;
	} else {
		global.JSENV = env;
	}
}(this, %s));
`

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
		os.Setenv("RISE_ENV", riseEnv)
	}

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}

	mimetypes.Register()
}

var (
	S3 filetransfer.FileTransfer = filetransfer.NewS3(s3client.PartSize, s3client.MaxUploadParts)

	errUnexpectedState = errors.New("deployment is in unexpected state")
)

func Work(data []byte) error {
	d := &messages.DeployJobData{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	db, err := dbconn.DB()
	if err != nil {
		return err
	}

	depl := &deployment.Deployment{}
	if err := db.First(depl, d.DeploymentID).Error; err != nil {
		if err == gorm.RecordNotFound {
			return ErrRecordNotFound
		}
		return err
	}

	proj := &project.Project{}
	if err := db.Where("id = ?", depl.ProjectID).First(proj).Error; err != nil {
		if err == gorm.RecordNotFound {
			return ErrRecordNotFound
		}
		return err
	}

	acquired, err := proj.Lock(db)
	if err != nil {
		return err
	}

	if !acquired {
		return ErrProjectLocked
	}

	defer func() {
		if err := proj.Unlock(db); err != nil {
			log.Printf("failed to unlock project %d due to %v", proj.ID, err)
		}
	}()

	if proj.Name != "help" && proj.Name != "pubstorm-blog" && proj.Name != "pubstorm-www" && proj.Name != "nitrous-www" {
		var errorMessage = "Project deployments and new account sign ups are no longer accepted. For more information, please visit https://www.pubstorm.com/"
		depl.ErrorMessage = &errorMessage
		depl.UpdateState(db, deployment.StateDeployFailed)
		return nil
	}

	// Return error if the deployment is in a state that bundle is not uploaded or not prepared for deploying
	if depl.State == deployment.StateUploaded || depl.State == deployment.StatePendingUpload {
		return errUnexpectedState
	}

	prefixID := depl.PrefixID()

	if !d.SkipWebrootUpload {
		// Disallow re-deploying a deployed project.
		if depl.State == deployment.StateDeployed {
			return errUnexpectedState
		}

		archiveFormat := d.ArchiveFormat
		if archiveFormat == "" {
			archiveFormat = "tar.gz"
		}

		var bundlePath string
		if !d.UseRawBundle {
			bundlePath = "deployments/" + prefixID + "/optimized-bundle." + archiveFormat
		} else {
			// If this deployment uses a raw bundle from a previous deploy, use that.
			if depl.RawBundleID != nil {
				bun := &rawbundle.RawBundle{}
				if err := db.First(bun, *depl.RawBundleID).Error; err == nil {
					bundlePath = bun.UploadedPath
				}
			} else {
				bundlePath = "deployments/" + prefixID + "/raw-bundle." + archiveFormat
			}
		}

		f, err := ioutil.TempFile("", prefixID+"-optimized-bundle."+archiveFormat)
		if err != nil {
			return err
		}
		defer func() {
			f.Close()
			os.Remove(f.Name())
		}()

		if err := S3.Download(s3client.BucketRegion, s3client.BucketName, bundlePath, f); err != nil {
			return err
		}

		// webroot is a publicly readable directory on S3.
		webroot := "deployments/" + prefixID + "/webroot"

		// From http://docs.aws.amazon.com/AmazonS3/latest/dev/UsingMetadata.html#object-keys
		// Add @ as an exceptional
		r := regexp.MustCompile("[^0-9A-Za-z,!_'()\\.\\*\\-@]+")
		done := make(chan struct{})
		errCh := make(chan error)
		if archiveFormat == "tar.gz" {
			go func() {
				gr, err := gzip.NewReader(f)
				if err != nil {
					errCh <- ErrUnarchiveFailed
					return
				}
				defer gr.Close()
				tr := tar.NewReader(gr)

				for {
					hdr, err := tr.Next()
					if err != nil {
						if err == io.EOF {
							break
						}
						errCh <- err
						return
					}

					if hdr.FileInfo().IsDir() {
						continue
					}

					fileName := path.Clean(hdr.Name)
					remotePath := webroot + "/" + fileName

					// Skip file with invalid filename
					pathElements := strings.Split(fileName, string(filepath.Separator))
					isValidFileName := true
					for _, pathElement := range pathElements {
						if r.MatchString(pathElement) {
							isValidFileName = false
							break
						}
					}

					if !isValidFileName {
						log.Printf("filename contains invalid character: %q", fileName)
						continue
					}

					contentType := mime.TypeByExtension(filepath.Ext(fileName))
					if i := strings.Index(contentType, ";"); i != -1 {
						contentType = contentType[:i]
					}

					var rdr io.Reader = tr

					// Inject "watermark" that links to PubStorm website for HTML pages.
					// TODO We should do the watermarking and uploading in several worker
					// goroutines.
					if proj.Watermark &&
						contentType == "text/html" &&
						hdr.Size <= MaxFileSizeToWatermark {

						var err error
						rdr, err = injectWatermark(rdr)
						if err != nil {
							// Log and skip this file.
							log.Printf("failed to inject watermark to %q, err: %v", hdr.Name, err)
							continue
						}
					}

					if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, remotePath, rdr, contentType, "public-read"); err != nil {
						errCh <- err
						return
					}
				}

				close(done)
			}()
		} else if archiveFormat == "zip" {
			go func() {
				r, err := zip.OpenReader(f.Name())
				if err != nil {
					errCh <- ErrUnarchiveFailed
					return
				}
				defer r.Close()

				for _, file := range r.File {
					rc, err := file.Open()
					if err != nil {
						errCh <- err
						return
					}
					defer rc.Close()

					if file.FileInfo().IsDir() {
						continue
					}
					remotePath := webroot + "/" + file.Name

					contentType := mime.TypeByExtension(filepath.Ext(file.Name))
					if i := strings.Index(contentType, ";"); i != -1 {
						contentType = contentType[:i]
					}

					var rdr io.Reader = rc

					// Inject "watermark" that links to PubStorm website for HTML pages.
					// TODO We should do the watermarking and uploading in several worker
					// goroutines.
					if proj.Watermark &&
						contentType == "text/html" &&
						file.FileInfo().Size() <= MaxFileSizeToWatermark {

						var err error
						rdr, err = injectWatermark(rdr)
						if err != nil {
							// Log and skip this file.
							log.Printf("failed to inject watermark to %q, err: %v", file.Name, err)
							continue
						}
					}

					if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, remotePath, rdr, contentType, "public-read"); err != nil {
						errCh <- err
						return
					}
				}
				close(done)
			}()
		}

		select {
		case <-done:
		case err := <-errCh:
			return err
		case <-time.After(UploadTimeout):
			errorMessage := "Timed out due to too many files"
			depl.ErrorMessage = &errorMessage
			if err := depl.UpdateState(db, deployment.StateDeployFailed); err != nil {
				fmt.Printf("Failed to update deployment state for %s due to %v", prefixID, err)
			}

			return ErrTimeout
		}

		var envvars map[string]string
		if err := json.Unmarshal(depl.JsEnvVars, &envvars); err != nil {
			return err
		}

		if err := S3.Upload(s3client.BucketRegion,
			s3client.BucketName,
			webroot+"/jsenv.js",
			bytes.NewBufferString(fmt.Sprintf(jsenvFormat, depl.JsEnvVars)),
			"application/javascript",
			"public-read"); err != nil {
			return err
		}
	}

	// the metadata file is also publicly readable, do not put sensitive data
	metaJson, err := json.Marshal(struct {
		Prefix            string  `json:"prefix"`
		ForceHTTPS        bool    `json:"force_https,omitempty"`
		BasicAuthUsername *string `json:"basic_auth_username,omitempty"`
		BasicAuthPassword *string `json:"basic_auth_password,omitempty"`
	}{
		prefixID,
		proj.ForceHTTPS,
		proj.BasicAuthUsername,
		proj.EncryptedBasicAuthPassword,
	})

	if err != nil {
		return err
	}

	domainNames, err := proj.DomainNames(db)
	if err != nil {
		return err
	}

	// Upload metadata file for each domain.
	reader := bytes.NewReader(metaJson)
	for _, domain := range domainNames {
		reader.Seek(0, 0)
		if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, "domains/"+domain+"/meta.json", reader, "application/json", "public-read"); err != nil {
			return err
		}
	}

	if !d.SkipInvalidation {
		m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
			Domains: domainNames,
		})
		if err != nil {
			return err
		}

		if err := m.Publish(); err != nil {
			return err
		}
	}

	tx := db.Begin()
	if err := tx.Error; err != nil {
		return err
	}
	defer tx.Rollback()

	if err := depl.UpdateState(tx, deployment.StateDeployed); err != nil {
		return err
	}

	if err := tx.Model(project.Project{}).Where("id = ?", proj.ID).Update("active_deployment_id", &depl.ID).Error; err != nil {
		return err
	}

	// If project has exceeded its max number of deployments (N), we soft delete
	// deployments older than the last N deployments.
	if proj.MaxDeploysKept > 0 {
		if err := deployment.DeleteExceptLastN(tx, proj.ID, proj.MaxDeploysKept); err != nil {
			return err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	{
		var u user.User
		if err := db.First(&u, depl.UserID).Error; err == nil {
			var (
				event     = "Project Deployed"
				timeTaken = depl.DeployedAt.Sub(depl.CreatedAt)
				props     = map[string]interface{}{
					"projectName":        proj.Name,
					"deploymentId":       depl.ID,
					"deploymentPrefix":   depl.Prefix,
					"deploymentVersion":  depl.Version,
					"timeTakenInSeconds": int64(timeTaken / time.Second),
				}
				context map[string]interface{}
			)
			if err := common.Track(strconv.Itoa(int(u.ID)), event, "", props, context); err != nil {
				log.Printf("failed to track %q event for user ID %d, err: %v",
					event, u.ID, err)
			}
		}
	}

	return nil
}
