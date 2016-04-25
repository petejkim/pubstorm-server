package fake

import (
	"io"
	"io/ioutil"
	"time"
)

type S3 struct {
	UploadCalls    Calls
	DownloadCalls  Calls
	DeleteCalls    Calls
	DeleteAllCalls Calls
	CopyCalls      Calls

	UploadError    error
	DownloadError  error
	DeleteError    error
	DeleteAllError error
	CopyError      error

	UploadTimeout time.Duration

	DownloadContent []byte
}

func (s *S3) Upload(region, bucket, key string, body io.Reader, contentType, acl string) (err error) {
	var content []byte

	if s.UploadError == nil {
		// If io.Reader is from file, the position could be the middle of file content.
		// To make sure it reads all content from the file, we need to change the position to the beginning of the file.
		seeker, ok := body.(io.Seeker)
		if ok {
			if _, err := seeker.Seek(0, 0); err != nil {
				return err
			}
		}

		content, err = ioutil.ReadAll(body)
	} else {
		err = s.UploadError
	}

	s.UploadCalls.Add(List{region, bucket, key, body, contentType, acl}, List{err}, Map{
		"uploaded_content": content,
	})

	// This is to simulate slow uploading.
	time.Sleep(s.UploadTimeout)

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

func (s *S3) Copy(region, bucket, key, newKey string) (err error) {
	err = s.CopyError
	s.CopyCalls.Add(List{region, bucket, key, newKey}, List{err}, nil)
	return err
}
