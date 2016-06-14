package fake

import (
	"io"
	"io/ioutil"
)

type S3 struct {
	UploadCalls    Calls
	DownloadCalls  Calls
	DeleteCalls    Calls
	DeleteAllCalls Calls

	UploadError    error
	DownloadError  error
	DeleteError    error
	DeleteAllError error

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

func (s *S3) Delete(region, bucket string, keys ...string) (err error) {
	err = s.DeleteError
	arglist := List{region, bucket}
	for _, key := range keys {
		arglist = append(arglist, key)
	}

	s.DeleteCalls.Add(arglist, List{err}, nil)
	return err
}

func (s *S3) DeleteAll(region, bucket, prefix string) error {
	err := s.DeleteAllError
	argList := List{region, bucket, prefix}

	s.DeleteAllCalls.Add(argList, List{err}, nil)
	return err
}
