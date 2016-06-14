package main

import (
	"os"
	"os/user"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

const jobName = "purge-deleted-deploys"

var fields = log.Fields{"job": jobName}

var (
	S3 filetransfer.FileTransfer = filetransfer.NewS3(s3client.PartSize, s3client.MaxUploadParts)
)

func init() {
	riseEnv := os.Getenv("RISE_ENV")
	if riseEnv == "" {
		riseEnv = "development"
		os.Setenv("RISE_ENV", riseEnv)
	}

	if riseEnv != "test" {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required!")
		}
	}
}

func main() {
	if u, err := user.Current(); err == nil {
		fields["user"] = u.Username
	}
	log.WithFields(fields).WithField("event", "start").
		Infof("Purging deleted deployments from S3...")

	db, err := dbconn.DB()
	if err != nil {
		log.WithFields(fields).Fatalf("failed to initialize db, err: %v", err)
	}

	depls, err := findSoftDeletedDeployments(db)
	if err != nil {
		log.WithFields(fields).Fatalf("failed to retrieve soft deleted deployments from db", err)
	}
	if len(depls) == 0 {
		log.WithFields(fields).WithField("event", "completed").Infof("No deployments to purge, exiting")
		os.Exit(0)
	}

	log.WithFields(fields).Infof("Found %d deployments to purge", len(depls))

	var (
		wg       sync.WaitGroup
		jobs     = make(chan *deployment.Deployment, len(depls))
		nWorkers = 5
	)

	for i := 0; i < nWorkers; i++ {
		go purger(db, &wg, jobs)
	}

	for i, depl := range depls {
		log.WithFields(fields).Infof("[%d/%d] Adding to purge queue: %v",
			i+1, len(depls), depl)
		wg.Add(1)
		jobs <- depl
	}

	wg.Wait()

	log.WithFields(fields).WithField("event", "completed").Infof("Successfully purged %d deployments", len(depls))
}

func findSoftDeletedDeployments(db *gorm.DB) ([]*deployment.Deployment, error) {
	depls := []*deployment.Deployment{}
	err := db.Unscoped().
		Where("deleted_at IS NOT NULL").
		Where("purged_at IS NULL").
		Where("state = ?", deployment.StateDeployed).
		Find(&depls).Error
	if err != nil {
		return nil, err
	}

	return depls, nil
}

func purger(db *gorm.DB, wg *sync.WaitGroup, jobs chan *deployment.Deployment) {
	for depl := range jobs {
		log.WithFields(fields).Infof("Purging deployment %v", depl)

		if err := purgeFromS3(db, depl); err != nil {
			log.WithFields(fields).Errorf("failed to purge deployment %s, err: %v", depl, err)
		}
		wg.Done()
	}
}

func purgeFromS3(db *gorm.DB, depl *deployment.Deployment) error {
	prefix := "deployments/" + depl.PrefixID()
	err := S3.DeleteAll(s3client.BucketRegion, s3client.BucketName, prefix)
	if err != nil {
		return err
	}

	return db.Model(depl).Unscoped().UpdateColumn("purged_at", time.Now()).Error
}
