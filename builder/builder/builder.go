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
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/apiserver/models/template"
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

	errUnexpectedState  = errors.New("deployment is in unexpected state")
	ErrProjectLocked    = errors.New("project is locked")
	ErrOptimizerTimeout = errors.New("Timed out on optimizing assets. This might happen due to too large asset files. We will continue without optimizing your assets.")
	ErrRecordNotFound   = errors.New("project or deployment is deleted")

	OptimizerCmd = func(containerName string, srcDir string, domainNames []string) *exec.Cmd {
		return exec.Command("docker", "run", "--name", containerName, "-v", srcDir+":"+OptimizePath, "-e", "DOMAIN_NAMES_WITH_PROTOCOL="+strings.Join(domainNames, ","), "--rm", OptimizerDockerImage)
	}

	OptimizerTimeout = 5 * 60 * time.Second // 5 mins
)

func Work(data []byte) error {
	d := &messages.BuildJobData{}
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

	if depl.State != deployment.StatePendingBuild {
		return errUnexpectedState
	}

	// There are 3 possible sources for the bundle (i.e. the files to be
	// deployed):
	//   1. A template.
	//   2. A raw bundle from a previous deployment.
	//   3. A raw bundle that was uploaded for this deployment.
	var bundlePath string

	// If deployment is for a template, we download it and use it.
	if depl.TemplateID != nil {
		tmpl := &template.Template{}
		if err := db.First(tmpl, *depl.TemplateID).Error; err == nil {
			bundlePath = tmpl.DownloadURL
		}
	}

	// If this deployment uses a raw bundle from a previous deploy, use that.
	if depl.RawBundleID != nil {
		bun := &rawbundle.RawBundle{}
		if err := db.First(bun, *depl.RawBundleID).Error; err == nil {
			bundlePath = bun.UploadedPath
		}
	}

	// At this point, if we still don't know the raw bundle's path, it must have
	// been uploaded to the deployment's prefix directory.
	prefixID := depl.PrefixID()
	if bundlePath == "" {
		bundlePath = "deployments/" + prefixID + "/raw-bundle.tar.gz"
	}

	f, err := ioutil.TempFile("", prefixID+"-raw-bundle.tar.gz")
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	dirName, err := ioutil.TempDir("", prefixID)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dirName)

	if err := S3.Download(s3client.BucketRegion, s3client.BucketName, bundlePath, f); err != nil {
		return err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
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

		entry.Close()
	}

	optimizedBundleTarball, err := ioutil.TempFile("", "optimized-bundle.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(optimizedBundleTarball.Name())

	deployJobMsg := messages.DeployJobData{
		DeploymentID: depl.ID,
	}

	nextState := deployment.StateBuilt

	// Optimize assets
	domainNames, err := proj.DomainNamesWithProtocol(db)
	if err != nil {
		return err
	}

	output, err := runOptimizer(fmt.Sprintf("%s-%d", prefixID, time.Now().Unix()), dirName, domainNames)
	if err == nil {
		var errorMessages []string
		outputs := strings.Split(output, "\n")
		for _, output := range outputs {
			if strings.HasPrefix(output, ErrorMessagePrefix) {
				errorMessages = append(errorMessages, strings.TrimLeft(output, ErrorMessagePrefix))
			}
		}

		if len(errorMessages) > 0 {
			nextState = deployment.StateBuildFailed
			errorMessage := strings.Join(errorMessages, "\n")
			log.Printf("error on optimizing: %v", errorMessage)
		}

		if err := pack(optimizedBundleTarball, dirName); err != nil {
			return err
		}

		if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, "deployments/"+prefixID+"/optimized-bundle.tar.gz", optimizedBundleTarball, "", "private"); err != nil {
			return err
		}

	} else if err == ErrOptimizerTimeout {
		if err := depl.UpdateState(db, deployment.StateBuildFailed); err != nil {
			return err
		}

		nextState = deployment.StateBuildFailed
		errorMessage := ErrOptimizerTimeout.Error()
		depl.ErrorMessage = &errorMessage
		deployJobMsg.UseRawBundle = true
	} else {
		return err
	}

	if err := depl.UpdateState(db, nextState); err != nil {
		return err
	}

	j, err := job.NewWithJSON(queues.Deploy, &deployJobMsg)
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

func pack(writer io.Writer, dirName string) error {
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

	walk := func(absPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dirName, absPath)
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(fi, relPath)
		if err != nil {
			return err
		}
		hdr.Name = relPath

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

		return nil
	}

	err := filepath.Walk(dirName, walk)
	if err != nil {
		return err
	}

	return nil
}

func runOptimizer(containerName, srcDir string, domainNames []string) (output string, err error) {
	outCh := make(chan string)
	errCh := make(chan error)
	cmd := OptimizerCmd(containerName, srcDir, domainNames)

	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			combinedErr := err
			if output != "" {
				combinedErr = errors.New(err.Error() + ":" + output)
			}
			errCh <- combinedErr
		}
		outCh <- string(out)
	}()

	select {
	case output := <-outCh:
		return output, nil
	case err := <-errCh:
		return "", err
	case <-time.After(OptimizerTimeout):
		if _, err := exec.Command("docker", "rm", "-f", containerName).CombinedOutput(); err != nil {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}

		return "", ErrOptimizerTimeout
	}
}
