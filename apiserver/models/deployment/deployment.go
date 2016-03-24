package deployment

import (
	"fmt"

	"github.com/jinzhu/gorm"
)

const (
	StatePendingUpload = "pending_upload"
	StateUploaded      = "uploaded"
	StatePendingDeploy = "pending_deploy"
	StateDeployed      = "deployed"
)

type Deployment struct {
	gorm.Model

	State string `sql:"default:'pending_upload'"`
	// 4-digit hex hash is used to ensure files are uploaded across multiple partitions in S3
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/request-rate-perf-considerations.html
	Prefix string `sql:"default:encode(gen_random_bytes(2), 'hex')"`

	ProjectID uint
	UserID    uint
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

// Returns prefix and ID in <prefix>-<id> format
func (d *Deployment) PrefixID() string {
	return fmt.Sprintf("%s-%d", d.Prefix, d.ID)
}
