package uploader

import (
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3Uploader struct {
	partSize int64
	maxParts int
}

func New(partSize int64, maxParts int) *S3Uploader {
	return &S3Uploader{partSize, maxParts}
}

func (s *S3Uploader) Upload(region string, body io.Reader, bucket, key string) (err error) {
	ss := session.New(&aws.Config{Region: aws.String(region)})
	uploader := s3manager.NewUploader(ss, func(u *s3manager.Uploader) {
		u.PartSize = s.partSize
		u.MaxUploadParts = s.maxParts
	})

	_, err = uploader.Upload(&s3manager.UploadInput{
		Body:   body,
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return
}
