package project_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "project")
}

var _ = Describe("Project", func() {
	var (
		u   *user.User
		db  *gorm.DB
		err error

		proj *project.Project
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u = factories.User(db)
		proj = factories.Project(db, u)
	})

	Describe("Validate()", func() {
		DescribeTable("validates name",
			func(name, nameErr string) {
				proj.Name = name
				errors := proj.Validate()

				if nameErr == "" {
					Expect(errors).To(BeNil())
				} else {
					Expect(errors).NotTo(BeNil())
					Expect(errors["name"]).To(Equal(nameErr))
				}
			},

			Entry("normal", "abc", ""),
			Entry("allows hyphens", "good-one", ""),
			Entry("allows multiple hyphens", "hello-world--foobar", ""),
			Entry("disallows starting with a hyphen", "-abc", "is invalid"),
			Entry("disallows ending with a hyphen", "abc-", "is invalid"),
			Entry("disallows spaces", "good one", "is invalid"),
			Entry("disallows special characters", "good&one", "is invalid"),
			Entry("disallows multiline regex attack", "abc\ndef", "is invalid"),
			Entry("disallows names shorter than 3 characters", "aa", "is too short (min. 3 characters)"),
			Entry("disallows names longer than 63 characters", strings.Repeat("a", 64), "is too long (max. 63 characters)"),
		)
	})

	Describe("FindByName()", func() {
		Context("when the project by the given name exists", func() {
			It("returns project", func() {
				proj2, err := project.FindByName(db, proj.Name)
				Expect(err).To(BeNil())
				Expect(proj2.ID).To(Equal(proj.ID))
				Expect(proj2.Name).To(Equal(proj.Name))
			})
		})

		Context("when the project by the given name does not exist", func() {
			It("returns nil", func() {
				proj2, err := project.FindByName(db, proj.Name+"xx")
				Expect(err).To(BeNil())
				Expect(proj2).To(BeNil())
			})
		})
	})

	Describe("DomainNames()", func() {
		Context("there is no domains for the project", func() {
			It("only returns the default subdomain", func() {
				domainNames, err := proj.DomainNames(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{
					fmt.Sprintf("%s.%s", proj.Name, shared.DefaultDomain),
				}))
			})
		})

		Context("there are domains for the project", func() {
			BeforeEach(func() {
				dom1 := &domain.Domain{
					ProjectID: proj.ID,
					Name:      "foo-bar-express.com",
				}
				err := db.Create(dom1).Error
				Expect(err).To(BeNil())

				dom2 := &domain.Domain{
					ProjectID: proj.ID,
					Name:      "foobarexpress.com",
				}
				err = db.Create(dom2).Error
				Expect(err).To(BeNil())
			})

			It("returns all domains", func() {
				domainNames, err := proj.DomainNames(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{
					fmt.Sprintf("%s.%s", proj.Name, shared.DefaultDomain),
					"foo-bar-express.com",
					"foobarexpress.com",
				}))
			})
		})
	})

	Describe("CanAddDomain()", func() {
		var origMaxDomains int

		BeforeEach(func() {
			origMaxDomains = shared.MaxDomainsPerProject
			shared.MaxDomainsPerProject = 2
		})

		AfterEach(func() {
			shared.MaxDomainsPerProject = origMaxDomains
		})

		Context("when the project has fewer than the max number of custom domains allowed", func() {
			It("returns true", func() {
				canCreate, err := proj.CanAddDomain(db)
				Expect(err).To(BeNil())
				Expect(canCreate).To(BeTrue())
			})
		})

		Context("when the project already has the max number of custom domains allowed", func() {
			BeforeEach(func() {
				for i := 0; i < shared.MaxDomainsPerProject; i++ {
					factories.Domain(db, proj)
				}
			})

			It("returns false", func() {
				canCreate, err := proj.CanAddDomain(db)
				Expect(err).To(BeNil())
				Expect(canCreate).To(BeFalse())
			})
		})
	})

	Describe("Lock()", func() {
		It("returns true if it successfully acquires a lock from the project", func() {
			proj.LockedAt = nil
			Expect(db.Save(proj).Error).To(BeNil())

			success, err := proj.Lock(db)
			Expect(err).To(BeNil())
			Expect(success).To(BeTrue())

			var updatedProj project.Project
			Expect(db.First(&updatedProj, proj.ID).Error).To(BeNil())
			Expect(updatedProj.LockedAt).NotTo(BeNil())
		})

		It("returns false if it fails acquires a lock from the project", func() {
			currentTime := time.Now()
			proj.LockedAt = &currentTime
			Expect(db.Save(proj).Error).To(BeNil())

			success, err := proj.Lock(db)
			Expect(err).To(BeNil())
			Expect(success).To(BeFalse())
		})
	})

	Describe("Unlock()", func() {
		It("unlocks the project", func() {
			currentTime := time.Now()
			proj.LockedAt = &currentTime
			Expect(db.Save(proj).Error).To(BeNil())

			Expect(proj.Unlock(db)).To(BeNil())

			var updatedProj project.Project
			Expect(db.First(&updatedProj, proj.ID).Error).To(BeNil())
			Expect(updatedProj.LockedAt).To(BeNil())
		})
	})

	Describe("AddCollaborator()", func() {
		It("returns an error when adding the project owner as a collaborator", func() {
			err := proj.AddCollaborator(db, u)
			Expect(err).To(Equal(project.ErrCollaboratorIsOwner))
		})

		Context("when user is already a collaborator", func() {
			var anotherU *user.User

			BeforeEach(func() {
				anotherU = factories.User(db)
				err := proj.AddCollaborator(db, anotherU)
				Expect(err).To(BeNil())
			})

			It("returns an error when adding a user who is already a collabator", func() {
				err := proj.AddCollaborator(db, anotherU)
				Expect(err).To(Equal(project.ErrCollaboratorAlreadyExists))
			})
		})

		Context("with another user", func() {
			var anotherU *user.User

			BeforeEach(func() {
				anotherU = factories.User(db)
			})

			It("adds the user as a collaborator", func() {
				err := proj.AddCollaborator(db, anotherU)
				Expect(err).To(BeNil())
				Expect(proj.Collaborators).To(HaveLen(1))
				Expect(proj.Collaborators[0].ID).To(Equal(anotherU.ID))

				var p2 project.Project
				err = db.Preload("Collaborators").First(&p2, proj.ID).Error
				Expect(err).To(BeNil())

				Expect(p2.Collaborators).To(HaveLen(1))
				Expect(p2.Collaborators[0].ID).To(Equal(anotherU.ID))
			})
		})
	})

	Describe("RemoveCollaborator()", func() {
		var collabU, anotherU *user.User

		BeforeEach(func() {
			collabU = factories.User(db)
			anotherU = factories.User(db)

			err := proj.AddCollaborator(db, collabU)
			Expect(err).To(BeNil())
		})

		It("returns an error when removing a user who's not a collaborator", func() {
			err := proj.RemoveCollaborator(db, anotherU)
			Expect(err).To(Equal(project.ErrNotCollaborator))
		})

		It("removes the user as a collaborator", func() {
			var p2 project.Project
			err = db.Preload("Collaborators").First(&p2, proj.ID).Error
			Expect(err).To(BeNil())
			Expect(p2.Collaborators).To(HaveLen(1))

			err := p2.RemoveCollaborator(db, collabU)
			Expect(err).To(BeNil())
			Expect(p2.Collaborators).To(BeEmpty())

			var p3 project.Project
			err = db.Preload("Collaborators").First(&p3, proj.ID).Error
			Expect(err).To(BeNil())
			Expect(p3.Collaborators).To(BeEmpty())
		})
	})

	Describe("LoadCollaborators()", func() {
		It("loads collaborators from the database into .Collabators", func() {
			Expect(proj.Collaborators).To(BeEmpty())

			collabU := factories.User(db)
			err := proj.AddCollaborator(db, collabU)
			Expect(err).To(BeNil())

			var p2 project.Project
			err = db.First(&p2, proj.ID).Error
			Expect(err).To(BeNil())

			err = p2.LoadCollaborators(db)
			Expect(err).To(BeNil())

			Expect(p2.Collaborators).To(HaveLen(1))
		})

		Context("when user has no collaborators", func() {
			It("loads a nil slice", func() {
				Expect(proj.Collaborators).To(BeEmpty())

				err := proj.LoadCollaborators(db)
				Expect(err).To(BeNil())

				Expect(proj.Collaborators).To(BeNil())
			})
		})

		Context("when called twice", func() {
			It("does not load two copies of each collaborator", func() {
				collabU := factories.User(db)
				err := proj.AddCollaborator(db, collabU)
				Expect(err).To(BeNil())

				var p2 project.Project
				err = db.First(&p2, proj.ID).Error
				Expect(err).To(BeNil())

				err = p2.LoadCollaborators(db)
				Expect(err).To(BeNil())

				Expect(p2.Collaborators).To(HaveLen(1))

				err = p2.LoadCollaborators(db)
				Expect(err).To(BeNil())

				Expect(p2.Collaborators).To(HaveLen(1))
			})
		})
	})
})
