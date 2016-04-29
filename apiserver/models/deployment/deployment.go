package deployment

import (
	"errors"
	"fmt"
	"time"

	"github.com/jinzhu/gorm"
)

// Allowed deployment states.
const (
	StatePendingUpload   = "pending_upload"
	StateUploaded        = "uploaded"
	StatePendingDeploy   = "pending_deploy"
	StateDeployed        = "deployed"
	StatePendingRollback = "pending_rollback"
	StatePendingBuild    = "pending_build"
	StateBuilt           = "built"
	StateBuildFailed     = "build_failed"
)

// Errors returned from this package.
var (
	ErrInvalidState = errors.New("state is not valid")
)

// Deployment is a database model representing a particular deploy of a Project.
type Deployment struct {
	gorm.Model

	State string `sql:"default:'pending_upload'"`
	// 4-digit hex hash is used to ensure files are uploaded across multiple partitions in S3
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/request-rate-perf-considerations.html
	Prefix  string `sql:"default:encode(gen_random_bytes(2), 'hex')"`
	Version int64

	ProjectID   uint
	UserID      uint
	RawBundleID *uint

	DeployedAt *time.Time
	PurgedAt   *time.Time

	ErrorMessage *string
}

// JSON specifies which fields of a deployment will be marshaled to JSON.
type JSON struct {
	ID           uint       `json:"id"`
	State        string     `json:"state"`
	Version      int64      `json:"version"`
	Active       bool       `json:"active,omitempty"`
	DeployedAt   *time.Time `json:"deployed_at,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
}

// AsJSON returns a struct that can be converted to JSON
func (d *Deployment) AsJSON() *JSON {
	return &JSON{
		ID:           d.ID,
		State:        d.State,
		Version:      d.Version,
		DeployedAt:   d.DeployedAt,
		ErrorMessage: d.ErrorMessage,
	}
}

// PrefixID returns prefix and ID in <prefix>-<id> format
func (d *Deployment) PrefixID() string {
	return fmt.Sprintf("%s-%d", d.Prefix, d.ID)
}

// PreviousCompletedDeployment returns previous deployment of current deployment
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

// CompletedDeployments returns completed deployments up to the given limit.
// A limit of 0 implies no limit (i.e. all deployments will be returned).
// Apologies for the magic number, but who'd ask for 0 deployments anyway.
func CompletedDeployments(db *gorm.DB, projectID, limit uint) ([]*Deployment, error) {
	qLimit := int(limit)
	if qLimit == 0 {
		qLimit = -1 // Gorm uses a limit of -1 to "disable" LIMIT clauses.
	}

	var depls []*Deployment
	if err := db.Limit(qLimit).Where("project_id = ? AND state = ?", projectID, StateDeployed).Order("deployed_at DESC").Find(&depls).Error; err != nil {
		return nil, err
	}
	return depls, nil
}

// DeleteExceptLastN deletes all but the last n deployed deployments.
func DeleteExceptLastN(db *gorm.DB, projectID, n uint) error {
	q := db.Exec(`
		UPDATE deployments
		SET deleted_at = now()
		WHERE
			project_id = ?
			AND state = ?
			AND deleted_at IS NULL
			AND deployed_at <= (
				SELECT deployed_at FROM deployments
				WHERE
					project_id = ?
					AND state = ?
					AND deleted_at IS NULL
				ORDER BY deployed_at DESC
				LIMIT 1 OFFSET ?
			);`, projectID, StateDeployed, projectID, StateDeployed, n)
	return q.Error
}

// UpdateState updates deployment state
func (d *Deployment) UpdateState(db *gorm.DB, state string) error {
	if !isValidState(state) {
		return ErrInvalidState
	}

	q := db.Model(Deployment{}).Where("id = ?", d.ID).Update("state", state)
	if state == StateDeployed {
		q = q.Update("deployed_at", gorm.Expr("now()"))
	}

	if state == StateBuildFailed {
		q = q.Update("error_message", d.ErrorMessage)
	}
	if state == StateUploaded && d.RawBundleID != nil {
		q = q.Update("raw_bundle_id", d.RawBundleID)
	}

	if err := q.Scan(d).Error; err != nil {
		return err
	}

	return nil
}

func (d *Deployment) String() string {
	return fmt.Sprintf("v%d of project %d", d.Version, d.ProjectID)
}

func isValidState(state string) bool {
	return StatePendingUpload == state ||
		StateUploaded == state ||
		StatePendingDeploy == state ||
		StateDeployed == state ||
		StatePendingRollback == state ||
		StatePendingBuild == state ||
		StateBuilt == state ||
		StateBuildFailed == state
}
