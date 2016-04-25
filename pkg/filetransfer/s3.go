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

func (s *S3) Upload(region, bucket, key string, body io.Reader, contentType, acl string) error {
	sess := session.New(&aws.Config{Region: aws.String(region)})
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		if s.partSize != 0 {
			u.PartSize = s.partSize
		}
		if s.maxUploadParts != 0 {
			u.MaxUploadParts = s.maxUploadParts
		}
	})

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if acl == "" {
		acl = "private"
	}

	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        body,
		ACL:         aws.String(acl),
		ContentType: aws.String(contentType),
	})
	return err
}

func (s *S3) Download(region, bucket, key string, out io.WriterAt) error {
	sess := session.New(&aws.Config{Region: aws.String(region)})
	downloader := s3manager.NewDownloader(sess, func(d *s3manager.Downloader) {
		if s.partSize != 0 {
			d.PartSize = s.partSize
		}
	})

	_, err := downloader.Download(out, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *S3) Delete(region, bucket string, keys ...string) error {
	svc := s3.New(session.New(&aws.Config{Region: aws.String(region)}))

	var objects []*s3.ObjectIdentifier
	for _, key := range keys {
		oi := &s3.ObjectIdentifier{
			Key:       aws.String(key),
			VersionId: nil,
		}

		objects = append(objects, oi)
	}

	params := &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3.Delete{Objects: objects},
	}

	_, err := svc.DeleteObjects(params)
	return err
}

func (s *S3) DeleteAll(region, bucket, prefix string) error {
	svc := s3.New(session.New(&aws.Config{Region: aws.String(region)}))

	listInput := &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	// This is slightly complex because ListObjectsPages() accepts a function
	// that gets called for every page returned. The function should return
	// false to stop iterating.
	var fnErr error
	err := svc.ListObjectsPages(listInput, func(res *s3.ListObjectsOutput, lastPage bool) (shouldContinue bool) {

		if len(res.Contents) == 0 {
			return false // Stop iterating.
		}

		objIdentifiers := make([]*s3.ObjectIdentifier, 0, len(res.Contents))
		for _, obj := range res.Contents {
			objIdentifiers = append(objIdentifiers, &s3.ObjectIdentifier{
				Key:       obj.Key,
				VersionId: nil,
			})
		}

		_, fnErr = svc.DeleteObjects(&s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3.Delete{Objects: objIdentifiers},
		})
		if fnErr != nil {
			return false // Stop iterating.
		}

		return !lastPage
	})
	if err != nil {
		return err
	}
	if fnErr != nil {
		return fnErr
	}

	return nil
}

func (s *S3) Copy(region, bucket, key, newKey string) (err error) {
	svc := s3.New(session.New(&aws.Config{Region: aws.String(region)}))

	_, err = svc.CopyObject(&s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(newKey),
		CopySource: aws.String(bucket + "/" + key),
	})

	return err
}
