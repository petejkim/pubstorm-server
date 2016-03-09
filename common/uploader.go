package common

import (
	"io"

	"github.com/nitrous-io/rise-server/pkg/uploader"
)

var Uploader uploader.Uploader = uploader.New(S3PartSize, S3MaxParts)

func Upload(path string, body io.Reader) (err error) {
	return Uploader.Upload(S3BucketRegion, body, S3BucketName, path)
}
