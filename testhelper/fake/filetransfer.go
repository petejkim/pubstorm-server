package fake

import (
	"io"
	"io/ioutil"
)

type FileTransfer struct {
	UploadCalls   Calls
	DownloadCalls Calls

	UploadError   error
	DownloadError error

	DownloadContent []byte
}

func (f *FileTransfer) Upload(region, bucket, key string, body io.Reader) (err error) {
	var content []byte

	if f.UploadError == nil {
		content, err = ioutil.ReadAll(body)
	} else {
		err = f.UploadError
	}

	f.UploadCalls.Add(List{region, bucket, key, body}, List{err}, Map{
		"uploaded_content": content,
	})

	return err
}

func (f *FileTransfer) Download(region, bucket, key string, out io.WriterAt) (err error) {
	if f.DownloadError == nil {
		_, err = out.WriteAt(f.DownloadContent, 0)
	} else {
		err = f.DownloadError
	}

	f.DownloadCalls.Add(List{region, bucket, key, out}, List{err}, nil)

	return err
}
