package deployment

import (
	"errors"
	"fmt"
	"time"

	"github.com/jinzhu/gorm"
)

const (
	StatePendingUpload = "pending_upload"
	StateUploaded      = "uploaded"
	StatePendingDeploy = "pending_deploy"
	StateDeployed      = "deployed"
)

var ErrInvalidState = errors.New("state is not valid")

type Deployment struct {
	gorm.Model

	State string `sql:"default:'pending_upload'"`
	// 4-digit hex hash is used to ensure files are uploaded across multiple partitions in S3
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/request-rate-perf-considerations.html
	Prefix string `sql:"default:encode(gen_random_bytes(2), 'hex')"`

	ProjectID uint
	UserID    uint

	DeployedAt *time.Time
}

type DeploymentJSON struct {
	ID         uint       `json:"id"`
	State      string     `json:"state"`
	Active     bool       `json:"active,omitempty"`
	DeployedAt *time.Time `json:"deployed_at,omitempty"`
}

// Returns a struct that can be converted to JSON
func (d *Deployment) AsJSON() *DeploymentJSON {
	return &DeploymentJSON{
		ID:         d.ID,
		State:      d.State,
		DeployedAt: d.DeployedAt,
	}
}

// Returns prefix and ID in <prefix>-<id> format
func (d *Deployment) PrefixID() string {
	return fmt.Sprintf("%s-%d", d.Prefix, d.ID)
}

// Returns previous deployment of current deployment
func (d *Deployment) PreviousCompletedDeployment(db *gorm.DB) (*Deployment, error) {
	var prevDepl Deployment

	if d.DeployedAt == nil || d.State != StateDeployed {
		return nil, nil
	}

	if err := db.Where("project_id = ? AND deployed_at IS NOT NULL AND deployed_at < ? AND state = ?", d.ProjectID, *d.DeployedAt, StateDeployed).
		Order("deployed_at DESC").
		First(&prevDepl).Error; err != nil {
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &prevDepl, nil
}

// Returns all completed deployments
func AllCompletedDeployments(db *gorm.DB, projectID uint) ([]*Deployment, error) {
	var depls []*Deployment
	if err := db.Where("project_id = ? AND state = ?", projectID, StateDeployed).Order("deployed_at DESC").Find(&depls).Error; err != nil {
		return nil, err
	}
	return depls, nil
}

// Updates deployment state
func (d *Deployment) UpdateState(db *gorm.DB, state string) error {
	if !isValidState(state) {
		return ErrInvalidState
	}

	query := "UPDATE deployments SET state = ? WHERE id = ? RETURNING *;"
	if state == StateDeployed {
		query = "UPDATE deployments SET state = ?, deployed_at = now() WHERE id = ? RETURNING *;"
	}

	if err := db.Raw(query, state, d.ID).Scan(d).Error; err != nil {
		return err
	}

	return nil
}

func isValidState(state string) bool {
	return StatePendingUpload == state ||
		StateUploaded == state ||
		StatePendingDeploy == state ||
		StateDeployed == state
}
