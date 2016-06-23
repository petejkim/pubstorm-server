package deployer

import (
	"archive/tar"
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
	"strconv"
	"strings"
	"time"

	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/mimetypes"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

var (
	ErrProjectLocked = errors.New("project is locked")
	ErrTimeout       = errors.New("failed to upload files due to timeout on uploading to s3")

	UploadTimeout = 3 * time.Minute
)

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
		return err
	}

	proj := &project.Project{}
	if err := db.Where("id = ?", depl.ProjectID).First(proj).Error; err != nil {
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

		bundlePath := "deployments/" + prefixID + "/optimized-bundle.tar.gz"
		if d.UseRawBundle {
			bundlePath = "deployments/" + prefixID + "/raw-bundle.tar.gz"
		}

		f, err := ioutil.TempFile("", prefixID+"-optimized-bundle.tar.gz")
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

		done := make(chan struct{})
		errCh := make(chan error)

		gr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gr)

		defer gr.Close()

		go func() {
			// webroot is a publicly readable directory on S3.
			webroot := "deployments/" + prefixID + "/webroot"

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

				contentType := mime.TypeByExtension(filepath.Ext(fileName))
				if i := strings.Index(contentType, ";"); i != -1 {
					contentType = contentType[:i]
				}

				if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, remotePath, tr, contentType, "public-read"); err != nil {
					errCh <- err
					return
				}
			}

			done <- struct{}{}
		}()

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
