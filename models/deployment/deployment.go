package deployment

import (
	"os/user"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/models/project"
)

type Deployment struct {
	gorm.Model

	State string `sql:"default:'pending'"`
	// 4-digit hex hash is used to ensure files are uploaded across multiple partitions in S3
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/request-rate-perf-considerations.html
	Prefix string `sql:"default:encode(gen_random_bytes(2), 'hex')"`

	ProjectID uint
	UserID    uint

	Project project.Project // belongs to project
	User    user.User       // belongs to user
}

// Returns a struct that can be converted to JSON
func (d *Deployment) AsJSON() interface{} {
	return struct {
		ID    uint   `json:"id"`
		State string `json:"state"`
	}{
		d.ID,
		d.State,
	}
}
