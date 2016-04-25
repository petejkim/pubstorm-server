package project

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
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
		Name string `json:"name"`
	}{
		p.Name,
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

	// We assume that nil is always sorted first.
	sort.Sort(sort.StringSlice(domNames))
	domNames[0] = p.DefaultDomainName()

	return domNames, nil
}

// Return Default domain
func (p *Project) DefaultDomainName() string {
	return p.Name + "." + shared.DefaultDomain
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

	collab := &collab.Collab{
		UserID:    u.ID,
		ProjectID: p.ID,
	}

	err := db.Create(&collab).Error

	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" && e.Constraint == "index_collabs_on_user_id_and_project_id" {
		return ErrCollaboratorAlreadyExists
	}

	return err
}

func (p *Project) RemoveCollaborator(db *gorm.DB, u *user.User) error {
	q := db.Delete(collab.Collab{}, "project_id = ? AND user_id = ?", p.ID, u.ID)
	if err := q.Error; err != nil {
		fmt.Println(err)
		return err
	}

	if q.RowsAffected == 0 {
		return ErrNotCollaborator
	}

	return nil
}

// Atomically increments version_counter and returns next deployment version
func (p *Project) NextVersion(db *gorm.DB) (int64, error) {
	r := struct{ V int64 }{}

	if err := db.Raw("UPDATE projects SET version_counter = version_counter + 1 WHERE id = ? RETURNING version_counter AS v;", p.ID).Scan(&r).Error; err != nil {
		return 0, err
	}

	return r.V, nil
}
