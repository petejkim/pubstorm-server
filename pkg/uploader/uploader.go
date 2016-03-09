package uploader

import "io"

type Uploader interface {
	Upload(region string, body io.Reader, bucket, key string) error
}
