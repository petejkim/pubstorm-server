package pushd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/push"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/githubapi"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

var (
	S3 filetransfer.FileTransfer = filetransfer.NewS3(s3client.PartSize, s3client.MaxUploadParts)

	ErrUnexpectedDeploymentState  = errors.New("deployment is in an unexpected state")
	ErrProjectConfigNotFound      = errors.New("GitHub Contents API response not HTTP 200")
	ErrProjectConfigInvalidFormat = errors.New("pubstorm.json file invalid")
	ErrGitHubArchiveProblem       = errors.New("could not download archive of repository from GitHub")
)

func Work(data []byte) error {
	d := &messages.PushJobData{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	db, err := dbconn.DB()
	if err != nil {
		return err
	}

	pu := &push.Push{}
	if err := db.First(pu, d.PushID).Error; err != nil {
		return err
	}

	rp := &repo.Repo{}
	if err := db.First(rp, pu.RepoID).Error; err != nil {
		return err
	}

	depl := &deployment.Deployment{}
	if err := db.First(depl, pu.DeploymentID).Error; err != nil {
		return err
	}

	if depl.State != deployment.StatePendingUpload {
		return ErrUnexpectedDeploymentState
	}

	proj := &project.Project{}
	if err := db.Where("id = ?", depl.ProjectID).First(proj).Error; err != nil {
		return err
	}

	pl := &githubapi.PushPayload{}
	if err := json.Unmarshal([]byte(pu.Payload), pl); err != nil {
		return err
	}

	projPath, err := fetchProjectPath(pl)
	if err != nil {
		switch err {
		case ErrProjectConfigNotFound:
			m := "Your GitHub repository does not contain a pubstorm.json file, aborting. Please check in the pubstorm.json file in the root of your repository."
			depl.ErrorMessage = &m
			if err := depl.UpdateState(db, deployment.StateDeployFailed); err != nil {
				fmt.Printf("Failed to update deployment state for deployment ID %d due to %v", depl.ID, err)
			}
		case ErrProjectConfigInvalidFormat:
			m := "Your repository's pubstorm.json is in an invalid format, aborting."
			depl.ErrorMessage = &m
			if err := depl.UpdateState(db, deployment.StateDeployFailed); err != nil {
				fmt.Printf("Failed to update deployment state for deployment ID %d due to %v", depl.ID, err)
			}
		}

		return err
	}

	// E.g. https://api.github.com/repos/PubStorm/pubstorm-www/{archive_format}{/ref}
	archiveURL := strings.Replace(pl.Repository.ArchiveURL, "{archive_format}", "tarball", 1)
	archiveURL = strings.Replace(archiveURL, "{/ref}", "/"+pl.After, 1)

	tmpDir, err := ioutil.TempDir("", "github-archive-"+depl.PrefixID())
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := fetchAndUnpackArchive(archiveURL, tmpDir, projPath); err != nil {
		return err
	}

	tarball, err := ioutil.TempFile("", "github-archive-raw-bundle")
	if err != nil {
		return err
	}
	defer os.Remove(tarball.Name())

	if err := gzipTarball(tarball, tmpDir); err != nil {
		return err
	}

	uploadKey := fmt.Sprintf("deployments/%s/raw-bundle.tar.gz", depl.PrefixID())
	if err := S3.Upload(s3client.BucketRegion, s3client.BucketName, uploadKey, tarball, "", "private"); err != nil {
		return err
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
		return err
	}

	if err := j.Enqueue(); err != nil {
		return err
	}

	newState := deployment.StatePendingBuild
	if proj.SkipBuild {
		newState = deployment.StatePendingDeploy
	}

	return depl.UpdateState(db, newState)
}

// fetchProjectPath downloads the pubstorm.json file from root dir of repository
// to determine the project path.
func fetchProjectPath(pl *githubapi.PushPayload) (string, error) {
	qs := url.Values{}
	qs.Add("ref", pl.After)
	cfgURL := fmt.Sprintf("%s/repos/%s/contents/pubstorm.json?%s",
		common.GitHubAPIHost, pl.Repository.FullName, qs.Encode())
	req, err := http.NewRequest("GET", cfgURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	if common.GitHubAPIToken != "" {
		req.Header.Set("Authorization", "token "+common.GitHubAPIToken)
	}
	cl := &http.Client{Timeout: 2 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", ErrProjectConfigNotFound
	}
	var j struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(res.Body).Decode(&j); err != nil {
		return "", ErrProjectConfigInvalidFormat
	}

	return j.Path, nil
}

// fetchAndUnpackArchive downloads the gzipped tarball from the given URL and
// unpacks only the given subdirectory to the root of the dst directory.
//
// We could optimize the download by performing a sparse checkout, so that we
// only fetch the contents of the directory instead of the entire repo:
//   1. git init
//   2. git remote add origin https://github.com/chuyeow/chuyeow.github.io.git
//   3. git config --local core.sparseCheckout true
//   4. echo build/ >> .git/info/sparse-checkout
//   5. git pull origin master
func fetchAndUnpackArchive(url, dst, subdir string) error {
	cl := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if common.GitHubAPIToken != "" {
		req.Header.Set("Authorization", "token "+common.GitHubAPIToken)
	}
	res, err := cl.Do(req)
	if err != nil {
		log.Printf("error downloading archive of repo from GitHub, err: %v", err)
		return ErrGitHubArchiveProblem
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return ErrGitHubArchiveProblem
	}

	gr, err := gzip.NewReader(res.Body)
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

		fileName := path.Clean(hdr.Name)

		relPath, err := filepath.Rel(subdir, fileName)
		if err != nil {
			continue
		}
		if strings.HasPrefix(relPath, "../") {
			continue
		}

		// Strip subdir from path.
		folderPath := path.Dir(relPath)
		if err := os.MkdirAll(filepath.Join(dst, folderPath), 0755); err != nil {
			return err
		}

		targetFileName := filepath.Join(dst, relPath)
		entry, err := os.Create(targetFileName)
		if err != nil {
			return err
		}
		defer entry.Close()

		if _, err := io.Copy(entry, tr); err != nil {
			return err
		}
	}

	return nil
}

func gzipTarball(w io.Writer, dir string) error {
	gw := gzip.NewWriter(w)
	defer func() {
		gw.Flush()
		gw.Close()
	}()

	tw := tar.NewWriter(gw)
	defer func() {
		tw.Flush()
		tw.Close()
	}()

	walkFn := func(absPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir, absPath)
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

		f, err := os.Open(absPath)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		return nil
	}

	return filepath.Walk(dir, walkFn)
}
