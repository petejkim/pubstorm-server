package main

import (
	"os"
	"strconv"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/mailer"
)

const jobName = "digest-sender"

var (
	fields           = log.Fields{"job": jobName}
	daysAgo          = 0
	apiServer        = os.Getenv("APISERVER_URL")
	statsToken       = os.Getenv("STATS_TOKEN")
	sendgridUsername = os.Getenv("SENDGRID_USERNAME")
	sendgridPassword = os.Getenv("SENDGRID_PASSWORD")
	db               *gorm.DB
	sendgrid         *mailer.SendGridMailer
	nWorkers         = 4
)

func init() {
	if os.Getenv("POSTGRES_URL") == "" {
		log.Fatalln("POSTGRES_URL is not defined")
	}

	if statsToken == "" {
		log.Fatalln("STATS_TOKEN is not defined")
	}

	if apiServer == "" {
		log.Fatalln("APISERVER_URL is not defined")
	}

	if os.Getenv("SENDGRID_USERNAME") == "" || os.Getenv("SENDGRID_PASSWORD") == "" {
		log.Fatalln("SENDGRID_USERNAME or SENDGRID_PASSWORD are not defined")
	}

	// It should be always 0, but this is useful for debugging.
	if os.Getenv("DAYS_AGO") != "" {
		m, err := strconv.Atoi(os.Getenv("DAYS_AGO"))
		if err == nil {
			daysAgo = m
		}
	}
}

func main() {
	var err error
	db, err = dbconn.DB()
	if err != nil {
		log.Errorln(err)
	}
	sendgrid = mailer.NewSendGridMailer(sendgridUsername, sendgridPassword)

	digestDate := time.Now().AddDate(0, 0, -daysAgo)
	digestYear, digestMonth, digestDay := digestDate.Date()

	// Only resend the digest is the date of the last digest is older than 6 days
	// Note: We are only comparing days without hours because the worker for that project
	// could have ben run on a different hour.
	limitDate := digestDate.AddDate(0, 0, -6)

	projects := []*project.Project{}
	sqlQuery := "last_digest_sent_at is null or date_trunc('day', last_digest_sent_at) < date_trunc('day', ?)"
	if r := db.Where(sqlQuery, limitDate).Find(&projects); r.Error != nil {
		log.Fatalln("Impossible to get project list")
	}

	var wg sync.WaitGroup
	projectsCh := make(chan *project.Project)
	for i := 0; i < nWorkers; i++ {
		go worker(&wg, projectsCh, digestYear, int(digestMonth), digestDay)
	}

	for _, project := range projects {
		wg.Add(1)
		log.Infof("Queueing stats for project %s\n", project.Name)
		projectsCh <- project
	}
	wg.Wait()
}

func worker(wg *sync.WaitGroup, ch chan *project.Project, year, month, day int) {
	for p := range ch {
		err := doJob(p, year, month, day)
		if err != nil {
			log.Errorln(err)
		}
		wg.Done()
	}
}
