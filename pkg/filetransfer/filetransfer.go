package filetransfer

import "io"

type FileTransfer interface {
	Upload(region, bucket, key string, body io.Reader, acl string) error
	Download(region, bucket, key string, out io.WriterAt) error
}
