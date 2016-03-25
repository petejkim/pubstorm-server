package fake

import (
	"io"
	"io/ioutil"
)

type S3 struct {
	UploadCalls   Calls
	DownloadCalls Calls
	DeleteCalls   Calls

	UploadError   error
	DownloadError error
	DeleteError   error

	DownloadContent []byte
}

func (s *S3) Upload(region, bucket, key string, body io.Reader, contentType, acl string) (err error) {
	var content []byte

	if s.UploadError == nil {
		content, err = ioutil.ReadAll(body)
	} else {
		err = s.UploadError
	}

	s.UploadCalls.Add(List{region, bucket, key, body, contentType, acl}, List{err}, Map{
		"uploaded_content": content,
	})

	return err
}

func (s *S3) Download(region, bucket, key string, out io.WriterAt) (err error) {
	if s.DownloadError == nil {
		_, err = out.WriteAt(s.DownloadContent, 0)
	} else {
		err = s.DownloadError
	}

	s.DownloadCalls.Add(List{region, bucket, key, out}, List{err}, nil)

	return err
}
