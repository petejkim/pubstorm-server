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
	"strings"

	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/mimetypes"
	"github.com/nitrous-io/rise-server/shared/s3client"
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
	err := json.Unmarshal(data, d)
	if err != nil {
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

	// Return error if the deployment is in a state that bundle is not uploaded or not prepared for deploying
	if depl.State == deployment.StateUploaded || depl.State == deployment.StatePendingUpload {
		return errUnexpectedState
	}

	prefixID := depl.PrefixID()

	if !d.SkipWebrootUpload {
		// We should not allow to re-upload for deployed project
		if depl.State == deployment.StateDeployed {
			return errUnexpectedState
		}

		rawBundle := "deployments/" + prefixID + "/raw-bundle.tar.gz"
		tmpFileName := prefixID + "-raw-bundle.tar.gz"

		f, err := ioutil.TempFile("", tmpFileName)
		if err != nil {
			return err
		}
		defer func() {
			f.Close()
			os.Remove(f.Name())
		}()

		if err := S3.Download(s3client.BucketRegion, s3client.BucketName, rawBundle, f); err != nil {
			return err
		}

		gr, err := gzip.NewReader(f)
		if err != nil {
			fmt.Println("could not unzip", err)
			return err
		}
		defer gr.Close()

		tr := tar.NewReader(gr)

		webroot := "deployments/" + prefixID + "/webroot"

		// webroot is publicly readable
		for {
			hdr, err := tr.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
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
				return err
			}
		}
	}

	proj := &project.Project{}
	if err := db.First(proj, depl.ProjectID).Error; err != nil {
		return err
	}

	var domainNames []string
	if proj.DefaultDomainEnabled {
		if err := uploadMataJSON(prefixID, proj.DefaultDomainName(), proj.DefaultDomainForceHTTPS); err != nil {
			return err
		}

		domainNames = append(domainNames, proj.DefaultDomainName())
	}

	var domains []*domain.Domain
	if err := db.Where("project_id = ?", proj.ID).Find(&domains).Error; err != nil {
		return err
	}

	for _, domain := range domains {
		if err := uploadMataJSON(prefixID, domain.Name, domain.ForceHTTPS); err != nil {
			return err
		}

		domainNames = append(domainNames, domain.Name)
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

	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
}

func uploadMataJSON(prefixID, domainName string, forceHTTPS bool) error {
	// the metadata file is also publicly readable, do not put sensitive data
	metaJson, err := json.Marshal(map[string]interface{}{
		"prefix":      prefixID,
		"force_https": forceHTTPS,
	})

	if err != nil {
		return err
	}

	reader := bytes.NewReader(metaJson)
	if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, "domains/"+domainName+"/meta.json", reader, "application/json", "public-read"); err != nil {
		return err
	}

	return nil
}
