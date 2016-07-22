package repo

import (
	"fmt"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
)

type Repo struct {
	gorm.Model

	ProjectID uint
	// UserID is the ID of the user who linked this repository to its project.
	// This could be the project owner or a collaborator.
	UserID uint

	URI    string `sql:"column:uri"`
	Branch string `sql:"default:'master'"`

	WebhookPath   string `sql:"default:encode(gen_random_bytes(16), 'hex')"`
	WebhookSecret string
}

func (r *Repo) AsJSON() interface{} {
	return struct {
		ProjectID     uint   `json:"project_id"`
		URI           string `json:"uri"`
		Branch        string `json:"branch"`
		WebhookURL    string `json:"webhook_url"`
		WebhookSecret string `json:"webhook_secret"`
	}{
		r.ProjectID,
		r.URI,
		r.Branch,
		r.WebhookURL(),
		r.WebhookSecret,
	}
}

func (r *Repo) WebhookURL() string {
	// Gin does not provide route generation, so unfortunately we have to
	// hardcode this and maintain it with routes.go.
	// See https://github.com/gin-gonic/gin/issues/357 to track this issue.
	return fmt.Sprintf("%s/hooks/github/%s", common.WebhookHost, r.WebhookPath)
}
