package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/stat"
)

type Stats struct {
	ProjectName string            `json:"project_name"`
	Stats       []stat.DomainStat `json:"stats"`
}

func getStats(url string) (*Stats, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	domainStat := &Stats{}

	err = json.Unmarshal(body, domainStat)
	if err != nil {
		return nil, err
	}

	return domainStat, nil
}

func doJob(p *project.Project, year int, month int, day int) error {
	// We are requesting the content via HTTP instead of using directly ES because in the future this can be cached
	var Url *url.URL
	Url, err := url.Parse(apiServer)
	if err != nil {
		return err
	}
	Url.Path += "/admin/stats"
	parameters := url.Values{}
	parameters.Add("token", statsToken)
	parameters.Add("project_id", strconv.FormatUint(uint64(p.ID), 10))
	parameters.Add("year", strconv.FormatInt(int64(year), 10))
	parameters.Add("month", strconv.FormatInt(int64(month), 10))
	parameters.Add("day", strconv.FormatInt(int64(day), 10))
	Url.RawQuery = parameters.Encode()

	projectStats, err := getStats(Url.String())
	if err != nil {
		return err
	}
	projectStats.ProjectName = p.Name

	// For now we don't send any digest if the user haven't received any request for that domain.
	if !isEmpty(projectStats) {
		var u user.User
		if r := db.Where("id = ?", p.UserID).First(&u); r.Error != nil {
			return r.Error
		}

		body, bodyHtml, err := generateEmailBody(projectStats)
		if err != nil {
			return err
		}

		subject := fmt.Sprintf("Pubstorm: digest for %s", p.Name)
		err = sendgrid.SendMail("Pubstorm Digest <noreply@pubstorm.com>", []string{u.Email}, []string{}, []string{}, "noreply@pubstorm.com", subject, body, bodyHtml)
		if err != nil {
			return err
		}
		log.Infof("Sent email: %s\n", subject)
	}

	// Update the project
	now := time.Now()
	p.LastDigestSentAt = &now
	if r := db.Save(p); r.Error != nil {
		return r.Error
	}
	return nil

}

func isEmpty(projectStats *Stats) bool {
	for _, domainStat := range projectStats.Stats {
		// Check if there is any request for that domain
		if domainStat.TotalRequests > 0 {
			return false
		}
	}
	return true
}
