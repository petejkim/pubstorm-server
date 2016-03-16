package filetransfer

import (
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3 struct {
	partSize       int64
	maxUploadParts int
}

func NewS3(partSize int64, maxUploadParts int) *S3 {
	return &S3{
		partSize:       partSize,
		maxUploadParts: maxUploadParts,
	}
}

func (s *S3) Upload(region, bucket, key string, body io.Reader, acl string) (err error) {
	sess := session.New(&aws.Config{Region: aws.String(region)})
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		if s.partSize != 0 {
			u.PartSize = s.partSize
		}
		if s.maxUploadParts != 0 {
			u.MaxUploadParts = s.maxUploadParts
		}
	})

	if acl == "" {
		acl = "private"
	}

	_, err = uploader.Upload(&s3manager.UploadInput{
		ACL:    aws.String(acl),
		Body:   body,
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return
}

func (s *S3) Download(region, bucket, key string, out io.WriterAt) (err error) {
	sess := session.New(&aws.Config{Region: aws.String(region)})
	downloader := s3manager.NewDownloader(sess, func(d *s3manager.Downloader) {
		if s.partSize != 0 {
			d.PartSize = s.partSize
		}
	})

	_, err = downloader.Download(out, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return
}
