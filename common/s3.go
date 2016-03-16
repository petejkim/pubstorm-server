package common

import (
	"io"

	"github.com/nitrous-io/rise-server/pkg/filetransfer"
)

var S3 filetransfer.FileTransfer = filetransfer.NewS3(S3PartSize, S3MaxUploadParts)

func Upload(path string, body io.Reader, contentType, acl string) (err error) {
	return S3.Upload(S3BucketRegion, S3BucketName, path, body, contentType, acl)
}

func Download(path string, out io.WriterAt) (err error) {
	return S3.Download(S3BucketRegion, S3BucketName, path, out)
}
