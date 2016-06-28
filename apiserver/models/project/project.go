package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/shared"

	"github.com/jinzhu/gorm"
)

var (
	projectNameRe = regexp.MustCompile(`\A[a-z0-9][a-z0-9\-]{1,61}[a-z0-9]\z`)

	ErrCollaboratorIsOwner       = errors.New("owner of project cannot be added as a collaborator")
	ErrCollaboratorAlreadyExists = errors.New("collaborator already exists")
	ErrNotCollaborator           = errors.New("user is not a collaborator of this project")

	ErrBasicAuthCredentialRequired = errors.New("basic_auth_username or basic_auth_password is empty")
)

type Project struct {
	gorm.Model

	Name                 string
	UserID               uint
	DefaultDomainEnabled bool `sql:"default:true"`
	ForceHTTPS           bool `sql:"column:force_https"`
	SkipBuild            bool
	MaxDeploysKept       uint
	LastDigestSentAt     *time.Time

	ActiveDeploymentID *uint // pointer to be nullable. remember to dereference by using *ActiveDeploymentID to get actual value
	BasicAuthUsername  *string
	BasicAuthPassword  string `sql:"-"`

	EncryptedBasicAuthPassword *string

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

	if (p.BasicAuthUsername != nil && *p.BasicAuthUsername != "") || p.BasicAuthPassword != "" {
		if p.BasicAuthUsername == nil || *p.BasicAuthUsername == "" {
			errors["basic_auth_username"] = "is required"
		} else if p.BasicAuthPassword == "" {
			errors["basic_auth_password"] = "is required"
		}
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

// Returns a struct that can be converted to JSON
func (p *Project) AsJSON() interface{} {
	return struct {
		Name                 string `json:"name"`
		DefaultDomainEnabled bool   `json:"default_domain_enabled"`
		ForceHTTPS           bool   `json:"force_https"`
		SkipBuild            bool   `json:"skip_build"`
	}{
		p.Name,
		p.DefaultDomainEnabled,
		p.ForceHTTPS,
		p.SkipBuild,
	}
}

// Returns list of domain names for this project
func (p *Project) DomainNames(db *gorm.DB) ([]string, error) {
	doms := []*domain.Domain{}
	if err := db.Order("name ASC").Where("project_id = ?", p.ID).Find(&doms).Error; err != nil {
		return nil, err
	}

	domNames := make([]string, len(doms))
	for i, dom := range doms {
		domNames[i] = dom.Name
	}
	sort.Sort(sort.StringSlice(domNames))

	// Always sort default domain at the front.
	if p.DefaultDomainEnabled {
		domNames = append([]string{p.DefaultDomainName()}, domNames...)
	}

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

// Destroy a project
func (p *Project) Destroy(db *gorm.DB) error {
	if err := db.Exec("UPDATE certs c SET deleted_at = now() FROM domains d WHERE c.domain_id = d.id AND d.project_id = ?", p.ID).Error; err != nil {
		return err
	}

	if err := db.Exec("UPDATE acme_certs c SET deleted_at = now() FROM domains d WHERE c.domain_id = d.id AND d.project_id = ?", p.ID).Error; err != nil {
		return err
	}

	if err := db.Delete(rawbundle.RawBundle{}, "project_id = ?", p.ID).Error; err != nil {
		return err
	}

	if err := db.Delete(domain.Domain{}, "project_id = ?", p.ID).Error; err != nil {
		return err
	}

	if err := db.Delete(p).Error; err != nil {
		return err
	}

	return nil
}

// Encrypt `BasicAuthPassword` with bcrypt
func (p *Project) EncryptBasicAuthPassword() error {
	if p.BasicAuthUsername == nil || *p.BasicAuthUsername == "" || p.BasicAuthPassword == "" {
		return ErrBasicAuthCredentialRequired
	}

	hasher := sha256.New()
	if _, err := hasher.Write([]byte(*p.BasicAuthUsername + ":" + p.BasicAuthPassword)); err != nil {
		return err
	}

	encryptedPassword := hex.EncodeToString(hasher.Sum(nil))
	p.EncryptedBasicAuthPassword = &encryptedPassword
	return nil
}

// Returns list of domain names with protocal for this project
func (p *Project) DomainNamesWithProtocol(db *gorm.DB) ([]string, error) {
	doms := []*struct {
		Name   string
		CertID *uint
	}{}

	if err := db.Table("domains").Select("domains.Name, certs.ID AS cert_id").Joins("LEFT JOIN certs ON domains.id = certs.domain_id AND certs.deleted_at is null").Where("project_id = ? AND domains.deleted_at is null", p.ID).Find(&doms).Error; err != nil {
		return nil, err
	}

	domNames := make([]string, len(doms))
	for i, dom := range doms {
		if dom.CertID != nil {
			domNames[i] = "https://" + dom.Name
		} else {
			domNames[i] = "http://" + dom.Name
		}
	}

	sort.Sort(sort.StringSlice(domNames))

	// We always use https for defaut domain
	if p.DefaultDomainEnabled {
		domainNameWithProtocol := "https://" + p.DefaultDomainName()
		domNames = append([]string{domainNameWithProtocol}, domNames...)
	}

	return domNames, nil
}
