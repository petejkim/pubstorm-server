package repo

import (
	"github.com/jinzhu/gorm"
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
		ProjectID   uint   `json:"project_id"`
		URI         string `json:"uri"`
		Branch      string `json:"branch"`
		WebhookPath string `json:"webhook_path"`
	}{
		r.ProjectID,
		r.URI,
		r.Branch,
		r.WebhookPath,
	}
}
