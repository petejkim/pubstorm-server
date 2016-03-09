package fake

import (
	"io"
	"io/ioutil"
)

type Uploader struct {
	Called int

	Region string
	Body   []byte
	Bucket string
	Key    string
	Err    error
}

func (u *Uploader) Upload(region string, reader io.Reader, bucket, key string) error {
	content, err := ioutil.ReadAll(reader)
	if err != nil {
		return err
	}

	u.Region = region
	u.Body = content
	u.Bucket = bucket
	u.Key = key

	u.Called++

	return u.Err
}
