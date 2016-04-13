package project

import (
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/shared"

	"github.com/jinzhu/gorm"
)

var (
	projectNameRe = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9\-]{1,61}[A-Za-z0-9]\z`)

	ErrCollaboratorIsOwner       = errors.New("owner of project cannot be added as a collaborator")
	ErrCollaboratorAlreadyExists = errors.New("collaborator already exists")
	ErrNotCollaborator           = errors.New("user is not a collaborator of this project")
)

type Project struct {
	gorm.Model

	Name   string
	UserID uint

	ActiveDeploymentID *uint // pointer to be nullable. remember to dereference by using *ActiveDeploymentID to get actual value

	LockedAt *time.Time

	Collaborators []user.User `gorm:"many2many:collabs;"`
}

// Validates Project, if there are invalid fields, it returns a map of
// <field, errors> and returns nil if valid
func (p *Project) Validate() map[string]string {
	errors := map[string]string{}

	if p.Name == "" {
		errors["name"] = "is required"
	} else if len(p.Name) < 3 {
		errors["name"] = "is too short (min. 3 characters)"
	} else if len(p.Name) > 63 {
		errors["name"] = "is too long (max. 63 characters)"
	} else if !projectNameRe.MatchString(p.Name) {
		errors["name"] = "is invalid"
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

// Returns a struct that can be converted to JSON
func (p *Project) AsJSON() interface{} {
	return struct {
		Name          string      `json:"name"`
		Collaborators []user.User `json:"collaborators,omitempty"`
	}{
		p.Name,
		p.Collaborators,
	}
}

// Returns list of domain names for this project
func (p *Project) DomainNames(db *gorm.DB) ([]string, error) {
	doms := []*domain.Domain{}
	if err := db.Order("name ASC").Where("project_id = ?", p.ID).Find(&doms).Error; err != nil {
		return nil, err
	}

	domNames := make([]string, len(doms)+1)
	for i, dom := range doms {
		domNames[i+1] = dom.Name
	}
	sort.Sort(sort.StringSlice(domNames))
	domNames[0] = p.Name + "." + shared.DefaultDomain

	return domNames, nil
}

// Find project by name
func FindByName(db *gorm.DB, name string) (proj *Project, err error) {
	proj = &Project{}
	q := db.Where("name = ?", name).First(proj)
	if err = q.Error; err != nil {
		if err == gorm.RecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return proj, nil
}

// Returns whether more domains can be added to this project
func (p *Project) CanAddDomain(db *gorm.DB) (bool, error) {
	var domainCount int
	if err := db.Model(domain.Domain{}).Where("project_id = ?", p.ID).Count(&domainCount).Error; err != nil {
		return false, err
	}

	if domainCount < shared.MaxDomainsPerProject {
		return true, nil
	}

	return false, nil
}

// Acquire a lock from the project for concurrent update
func (p *Project) Lock(db *gorm.DB) (bool, error) {
	q := db.Exec(`
		UPDATE projects
		SET locked_at = now()
		WHERE id IN (
			SELECT id FROM projects
			WHERE id = ? AND locked_at IS NULL
			FOR UPDATE
		);
	`, p.ID)

	if q.Error != nil {
		return false, q.Error
	}

	if q.RowsAffected == 0 {
		return false, nil
	}

	return true, nil
}

// Release the lock from the project for concurrent update
func (p *Project) Unlock(db *gorm.DB) error {
	return db.Exec(`
		UPDATE projects
		SET locked_at = NULL
		WHERE id IN (
			SELECT id FROM projects
			WHERE id = ? AND locked_at IS NOT NULL
			FOR UPDATE
		);
	`, p.ID).Error
}

func (p *Project) AddCollaborator(db *gorm.DB, u *user.User) error {
	if u.ID == p.UserID {
		return ErrCollaboratorIsOwner
	}

	if err := p.LoadCollaborators(db); err != nil {
		return err
	}

	// Check whether user is already a collaborator.
	for _, collab := range p.Collaborators {
		if collab.ID == u.ID {
			return ErrCollaboratorAlreadyExists
		}
	}

	return db.Model(p).Association("Collaborators").Append(u).Error
}

func (p *Project) RemoveCollaborator(db *gorm.DB, u *user.User) error {
	if err := p.LoadCollaborators(db); err != nil {
		return err
	}

	// Check whether user is actually a collaborator.
	var found bool
	for _, collab := range p.Collaborators {
		if collab.ID == u.ID {
			found = true
		}
	}

	if !found {
		return ErrNotCollaborator
	}

	return db.Model(p).Association("Collaborators").Delete(u).Error
}

// LoadCollaborators fetches associated collaborators from the database and
// populates .Collaborators. We need to do this because Gorm doesn't
// automatically load associations in its finders.
func (p *Project) LoadCollaborators(db *gorm.DB) error {
	// Gorm's Preload() function will naively append to the existing slice, so
	// we empty it first.
	p.Collaborators = nil

	return db.Preload("Collaborators").First(p, p.ID).Error
}
