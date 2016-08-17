package filetransfer

import "io"

type FileTransfer interface {
	Upload(region, bucket, key string, body io.Reader, contentType, acl string) error
	Download(region, bucket, key string, out io.WriterAt) error
	Delete(region, bucket string, keys ...string) error
	DeleteAll(region, bucket, prefix string) error
	Copy(region, bucket, srcKey, destKey string) error
}
