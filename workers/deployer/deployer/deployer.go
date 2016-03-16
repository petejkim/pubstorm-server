package deployer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nitrous-io/rise-server/pkg/filetransfer"
)

var (
	S3BucketRegion   = os.Getenv("S3_BUCKET_REGION")
	S3BucketName     = os.Getenv("S3_BUCKET_NAME")
	MaxUploadSize    = int64(1024 * 1024 * 1000) // 1 GiB
	S3PartSize       = int64(50 * 1024 * 1024)   // 50 MiB
	S3MaxUploadParts = int(math.Ceil(float64(MaxUploadSize) / float64(S3PartSize)))
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
	}

	if S3BucketRegion == "" {
		S3BucketRegion = "us-west-2"
	}

	if S3BucketName == "" {
		S3BucketName = "rise-development-usw2"
	}

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}
}

type jobData struct {
	DeploymentID     int64  `json:"deployment_id"`
	DeploymentPrefix string `json:"deployment_prefix"`
	ProjectName      string `json:"project_name"`
	Domain           string `json:"domain"`
}

var S3 filetransfer.FileTransfer = filetransfer.NewS3(S3PartSize, S3MaxUploadParts)

func Work(data []byte) error {
	d := &jobData{}
	err := json.Unmarshal(data, d)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s-%d", d.DeploymentPrefix, d.DeploymentID)
	rawBundle := "deployments/" + prefix + "/raw-bundle.tar.gz"
	tmpFileName := prefix + "-raw-bundle.tar.gz"

	f, err := ioutil.TempFile("", tmpFileName)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	if err := S3.Download(S3BucketRegion, S3BucketName, rawBundle, f); err != nil {
		return err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		fmt.Println("could not unzip", err)
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	webroot := "deployments/" + prefix + "/webroot"

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

		if err := S3.Upload(S3BucketRegion, S3BucketName, remotePath, tr, contentType, "public-read"); err != nil {
			return err
		}
	}

	// the metadata file is also publicly readable, do not put sensitive data
	metaJson := &bytes.Buffer{}
	if err := json.NewEncoder(metaJson).Encode(map[string]interface{}{
		"webroot": webroot,
	}); err != nil {
		return err
	}

	if err := S3.Upload(S3BucketRegion, S3BucketName, "domains/"+d.Domain+"/meta.json", metaJson, "application/json", "public-read"); err != nil {
		return err
	}

	return nil
}
