package push

import (
	"time"

	"github.com/jinzhu/gorm"
)

type Push struct {
	gorm.Model

	RepoID       uint
	DeploymentID uint

	Ref     string
	Payload string

	ProcessedAt *time.Time
}
