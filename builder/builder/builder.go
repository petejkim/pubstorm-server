package builder

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

const (
	OptimizePath         = "/tmp/optimizer/build"
	OptimizerDockerImage = "quay.io/nitrous/pubstorm-optimizer"
	ErrorMessagePrefix   = "[Error] "
)

var ErrProjectLocked = errors.New("project is locked")

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
}

var (
	S3 filetransfer.FileTransfer = filetransfer.NewS3(s3client.PartSize, s3client.MaxUploadParts)

	errUnexpectedState = errors.New("deployment is in unexpected state")
)

func Work(data []byte) error {
	d := &messages.BuildJobData{}
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

	if depl.State != deployment.StatePendingBuild {
		return errUnexpectedState
	}

	prefixID := depl.PrefixID()

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

	dirName, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}

	defer os.Remove(dirName)

	if err := S3.Download(s3client.BucketRegion, s3client.BucketName, rawBundle, f); err != nil {
		return err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		log.Println("could not unzip", err)
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	// webroot is publicly readable

	var (
		absPaths []string
		relPaths []string
	)

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

		folderPath := path.Dir(hdr.Name)
		if err := os.MkdirAll(filepath.Join(dirName, folderPath), 0755); err != nil {
			return err
		}

		fileName := path.Clean(hdr.Name)
		targetFileName := filepath.Join(dirName, fileName)
		entry, err := os.Create(targetFileName)
		if err != nil {
			return err
		}
		defer entry.Close()

		if _, err := io.Copy(entry, tr); err != nil {
			return err
		}

		relPaths = append(relPaths, fileName)
		absPaths = append(absPaths, targetFileName)
	}

	optimizedBundleTarball, err := ioutil.TempFile("", "optimized-bundle.tar.gz")
	if err != nil {
		return err
	}
	defer optimizedBundleTarball.Close()

	// Optimize assets
	out, err := exec.Command("docker", "run", "-v", dirName+":"+OptimizePath, OptimizerDockerImage).CombinedOutput()
	if err != nil {
		return err
	}

	var errorMessages []string
	outputs := strings.Split(string(out), "\n")
	fmt.Printf("output: %+v\n", outputs)
	for _, output := range outputs {
		if strings.HasPrefix(output, ErrorMessagePrefix) {
			errorMessages = append(errorMessages, strings.TrimLeft(output, ErrorMessagePrefix))
		}
	}

	if len(errorMessages) > 0 {
		fmt.Printf("error messages: %+v\n", errorMessages)
		if err := db.Model(deployment.Deployment{}).Where("id = ?", depl.ID).Update("error_message", strings.Join(errorMessages, "\n")).Error; err != nil {
			return err
		}
	}

	if err := pack(optimizedBundleTarball, absPaths, relPaths); err != nil {
		return err
	}

	if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, "deployments/"+prefixID+"/optimized-bundle.tar.gz", optimizedBundleTarball, "", "private"); err != nil {
		return err
	}

	if err := depl.UpdateState(db, deployment.StateBuilt); err != nil {
		return err
	}

	j, err := job.NewWithJSON(queues.Deploy, &messages.DeployJobData{
		DeploymentID: depl.ID,
	})

	if err != nil {
		return err
	}

	if err := j.Enqueue(); err != nil {
		return err
	}

	if err := depl.UpdateState(db, deployment.StatePendingDeploy); err != nil {
		return err
	}

	return nil
}

func pack(writer io.Writer, absPaths, relPaths []string) error {
	gw := gzip.NewWriter(writer)
	defer func() {
		gw.Flush()
		gw.Close()
	}()

	tw := tar.NewWriter(gw)
	defer func() {
		tw.Flush()
		tw.Close()
	}()

	for index, absPath := range absPaths {
		fi, err := os.Stat(absPath)
		if err != nil {
			return err
		}

		relPath := relPaths[index]

		hdr, err := tar.FileInfoHeader(fi, relPath)
		hdr.Name = relPath
		if err != nil {
			return err
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		ff, err := os.Open(absPath)
		if err != nil {
			return err
		}
		defer ff.Close()

		if _, err = io.Copy(tw, ff); err != nil {
			return err
		}
	}
	return nil
}
