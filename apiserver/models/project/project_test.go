package project_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/acmecert"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
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
			Entry("disallows uppercase letters", "Bad-one", "is invalid"),
			Entry("disallows starting with a hyphen", "-abc", "is invalid"),
			Entry("disallows ending with a hyphen", "abc-", "is invalid"),
			Entry("disallows spaces", "good one", "is invalid"),
			Entry("disallows special characters", "good&one", "is invalid"),
			Entry("disallows multiline regex attack", "abc\ndef", "is invalid"),
			Entry("disallows names shorter than 3 characters", "aa", "is too short (min. 3 characters)"),
			Entry("disallows names longer than 63 characters", strings.Repeat("a", 64), "is too long (max. 63 characters)"),
		)

		DescribeTable("validates basic auth credential",
			func(username, password, usernameErr, passwordErr string) {
				proj.BasicAuthUsername = &username
				proj.BasicAuthPassword = password
				errors := proj.Validate()

				if usernameErr == "" && passwordErr == "" {
					Expect(errors).To(BeNil())
				} else {
					Expect(errors).NotTo(BeNil())

					if usernameErr != "" {
						Expect(errors["basic_auth_username"]).To(Equal(usernameErr))
					}

					if passwordErr != "" {
						Expect(errors["basic_auth_password"]).To(Equal(passwordErr))
					}
				}
			},

			Entry("normal", "abc", "def", "", ""),
			Entry("missing both", "", "", "", ""),
			Entry("missing username", "", "def", "is required", ""),
			Entry("missing password", "abc", "", "", "is required"),
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
		Context("there are no domains for the project", func() {
			It("only returns the default subdomain", func() {
				domainNames, err := proj.DomainNames(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{proj.DefaultDomainName()}))
			})

			Context("when default domain is disabled", func() {
				BeforeEach(func() {
					proj.DefaultDomainEnabled = false
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("returns an empty slice", func() {
					domainNames, err := proj.DomainNames(db)
					Expect(err).To(BeNil())
					Expect(domainNames).To(BeEmpty())
				})
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

			It("returns all domains, including the default domain", func() {
				domainNames, err := proj.DomainNames(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{
					proj.DefaultDomainName(),
					"foo-bar-express.com",
					"foobarexpress.com",
				}))
			})

			Context("when default domain is disabled", func() {
				BeforeEach(func() {
					proj.DefaultDomainEnabled = false
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("returns all domains, excluding the default domain", func() {
					domainNames, err := proj.DomainNames(db)
					Expect(err).To(BeNil())
					Expect(domainNames).To(Equal([]string{
						"foo-bar-express.com",
						"foobarexpress.com",
					}))
				})
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
		var proj2 *project.Project

		BeforeEach(func() {
			proj2 = factories.Project(db, u)
		})

		It("returns an error when adding the project owner as a collaborator", func() {
			err := proj.AddCollaborator(db, u)
			Expect(err).To(Equal(project.ErrCollaboratorIsOwner))
		})

		Context("when user is already a collaborator", func() {
			var anotherU *user.User

			BeforeEach(func() {
				anotherU = factories.User(db)
				factories.Collab(db, proj, anotherU)
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

				cols := []collab.Collab{}
				err = db.Model(collab.Collab{}).Where("project_id = ?", proj.ID).Find(&cols).Error
				Expect(err).To(BeNil())
				Expect(len(cols)).To(Equal(1))
				Expect(cols[0].UserID).To(Equal(anotherU.ID))
				Expect(cols[0].ProjectID).To(Equal(proj.ID))

				// it doesn't affect other projects
				cols = []collab.Collab{}
				err = db.Model(collab.Collab{}).Where("project_id = ?", proj2.ID).Find(&cols).Error
				Expect(err).To(BeNil())
				Expect(len(cols)).To(Equal(0))
			})
		})
	})

	Describe("RemoveCollaborator()", func() {
		var (
			u2, u3, u4 *user.User
			proj2      *project.Project
		)

		BeforeEach(func() {
			proj2 = factories.Project(db, u)

			u2 = factories.User(db)
			u3 = factories.User(db)
			u4 = factories.User(db)

			factories.Collab(db, proj, u2)
			factories.Collab(db, proj, u3)
			factories.Collab(db, proj2, u2)
		})

		It("returns an error when removing a user who's not a collaborator", func() {
			err := proj.RemoveCollaborator(db, u4)
			Expect(err).To(Equal(project.ErrNotCollaborator))
		})

		It("removes the user as a collaborator", func() {
			err := proj.RemoveCollaborator(db, u2)
			Expect(err).To(BeNil())

			cols := []collab.Collab{}
			err = db.Model(collab.Collab{}).Where("project_id = ?", proj.ID).Find(&cols).Error
			Expect(err).To(BeNil())
			Expect(len(cols)).To(Equal(1))
			Expect(cols[0].UserID).To(Equal(u3.ID))
			Expect(cols[0].ProjectID).To(Equal(proj.ID))

			// it doesn't affect other projects
			cols = []collab.Collab{}
			err = db.Model(collab.Collab{}).Where("project_id = ?", proj2.ID).Find(&cols).Error
			Expect(err).To(BeNil())
			Expect(len(cols)).To(Equal(1))
			Expect(cols[0].UserID).To(Equal(u2.ID))
			Expect(cols[0].ProjectID).To(Equal(proj2.ID))
		})
	})

	Describe("NextVersion()", func() {
		var proj2 *project.Project

		BeforeEach(func() {
			proj2 = factories.Project(db, u)
		})

		It("atomically increments and returns version counter", func() {
			v, err := proj.NextVersion(db)
			Expect(err).To(BeNil())
			Expect(v).To(Equal(int64(1)))

			v, err = proj.NextVersion(db)
			Expect(err).To(BeNil())
			Expect(v).To(Equal(int64(2)))

			v, err = proj2.NextVersion(db)
			Expect(err).To(BeNil())
			Expect(v).To(Equal(int64(1)))

			v, err = proj.NextVersion(db)
			Expect(err).To(BeNil())
			Expect(v).To(Equal(int64(3)))

			v, err = proj2.NextVersion(db)
			Expect(err).To(BeNil())
			Expect(v).To(Equal(int64(2)))
		})
	})

	Describe("Destroy()", func() {
		var (
			proj  *project.Project
			proj2 *project.Project
		)

		BeforeEach(func() {
			proj = factories.Project(db, u)
			proj2 = factories.Project(db, u)
		})

		It("deletes associated domains and certs", func() {
			Expect(proj.Destroy(db)).To(BeNil())

			var count int
			Expect(db.Model(project.Project{}).Where("id = ?", proj.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(0))
		})

		Context("when a project has domains and certs", func() {
			var (
				dm1  *domain.Domain
				dm2  *domain.Domain
				bun1 *rawbundle.RawBundle

				dm3  *domain.Domain
				ct3  *cert.Cert
				bun2 *rawbundle.RawBundle
			)

			BeforeEach(func() {
				dm1 = factories.Domain(db, proj)
				dm2 = factories.Domain(db, proj)

				ct1 := &cert.Cert{
					DomainID:        dm1.ID,
					CertificatePath: "old/path",
					PrivateKeyPath:  "old/path",
				}
				Expect(db.Create(ct1).Error).To(BeNil())

				ct2 := &cert.Cert{
					DomainID:        dm2.ID,
					CertificatePath: "old/path",
					PrivateKeyPath:  "old/path",
				}
				Expect(db.Create(ct2).Error).To(BeNil())

				letsencryptCert := &acmecert.AcmeCert{
					DomainID:       dm1.ID,
					LetsencryptKey: "key1",
					PrivateKey:     "key2",
					Cert:           "cert",
				}
				Expect(db.Create(letsencryptCert).Error).To(BeNil())

				dm3 = factories.Domain(db, proj2)

				ct3 = &cert.Cert{
					DomainID:        dm3.ID,
					CertificatePath: "old/path",
					PrivateKeyPath:  "old/path",
				}
				Expect(db.Create(ct3).Error).To(BeNil())

				bun1 = factories.RawBundle(db, proj)
				bun2 = factories.RawBundle(db, proj2)
			})

			It("deletes associated domains, certs and raw bundles", func() {
				Expect(proj.Destroy(db)).To(BeNil())

				var count int
				Expect(db.Model(domain.Domain{}).Where("project_id = ?", proj.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(0))

				Expect(db.Model(rawbundle.RawBundle{}).Where("id = ?", bun1.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(0))

				Expect(db.Model(cert.Cert{}).Where("domain_id IN (?,?)", dm1.ID, dm2.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(0))

				Expect(db.Model(acmecert.AcmeCert{}).Where("domain_id IN (?,?)", dm1.ID, dm2.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(0))

				// Make sure it does not delete other project's domains and certs
				Expect(db.Model(domain.Domain{}).Where("id = ?", dm3.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(1))

				Expect(db.Model(rawbundle.RawBundle{}).Where("id = ?", bun2.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(1))

				Expect(db.Model(cert.Cert{}).Where("id = ?", ct3.ID).Count(&count).Error).To(BeNil())
				Expect(count).To(Equal(1))
			})
		})
	})

	Describe("EncryptBasicAuthPassword()", func() {
		var proj *project.Project

		BeforeEach(func() {
			proj = factories.Project(db, u)
			username := "hihihi"
			proj.BasicAuthUsername = &username
			proj.BasicAuthPassword = "hello"
		})

		It("encrypts basic auth password and set it to EncryptedBasicAuthPassword", func() {
			Expect(proj.EncryptBasicAuthPassword()).To(BeNil())

			hasher := sha256.New()
			_, err := hasher.Write([]byte("hihihi:hello"))
			Expect(err).To(BeNil())

			Expect(*proj.EncryptedBasicAuthPassword).To(Equal(hex.EncodeToString(hasher.Sum(nil))))
		})

		It("returns error if BasicAuthPassword is empty", func() {
			proj.BasicAuthPassword = ""
			Expect(proj.EncryptBasicAuthPassword()).To(Equal(project.ErrBasicAuthCredentialRequired))
			Expect(proj.EncryptedBasicAuthPassword).To(BeNil())
		})

		It("returns error if BasicAuthUsername is empty", func() {
			proj.BasicAuthUsername = nil
			Expect(proj.EncryptBasicAuthPassword()).To(Equal(project.ErrBasicAuthCredentialRequired))
			Expect(proj.EncryptedBasicAuthPassword).To(BeNil())
		})
	})

	Describe("DomainNamesWithProtocol()", func() {
		Context("there are no domains for the project", func() {
			It("only returns the default subdomain", func() {
				domainNames, err := proj.DomainNamesWithProtocol(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{"https://" + proj.DefaultDomainName()}))
			})

			Context("when default domain is disabled", func() {
				BeforeEach(func() {
					proj.DefaultDomainEnabled = false
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("returns an empty slice", func() {
					domainNames, err := proj.DomainNamesWithProtocol(db)
					Expect(err).To(BeNil())
					Expect(domainNames).To(BeEmpty())
				})
			})
		})

		Context("there are domains for the project", func() {
			var dom1 *domain.Domain

			BeforeEach(func() {
				dom1 = &domain.Domain{
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

			It("returns custom domains with 'http://'", func() {
				domainNames, err := proj.DomainNamesWithProtocol(db)
				Expect(err).To(BeNil())
				Expect(domainNames).To(Equal([]string{
					"https://" + proj.DefaultDomainName(),
					"http://foo-bar-express.com",
					"http://foobarexpress.com",
				}))
			})

			Context("when cert exists for some custom domains", func() {
				var ct *cert.Cert

				BeforeEach(func() {
					ct = factories.Cert(db, dom1)
				})

				It("returns custom domain name that has cert with 'https://'", func() {
					domainNames, err := proj.DomainNamesWithProtocol(db)
					Expect(err).To(BeNil())
					Expect(domainNames).To(Equal([]string{
						"https://" + proj.DefaultDomainName(),
						"http://foobarexpress.com",
						"https://foo-bar-express.com",
					}))
				})

				Context("when existing cert is soft-deleted", func() {
					BeforeEach(func() {
						Expect(db.Delete(ct).Error).To(BeNil())
					})

					It("returns custom domain name that has cert with 'https://'", func() {
						domainNames, err := proj.DomainNamesWithProtocol(db)
						Expect(err).To(BeNil())
						Expect(domainNames).To(Equal([]string{
							"https://" + proj.DefaultDomainName(),
							"http://foo-bar-express.com",
							"http://foobarexpress.com",
						}))
					})
				})
			})

			Context("when default domain is disabled", func() {
				BeforeEach(func() {
					proj.DefaultDomainEnabled = false
					Expect(db.Save(proj).Error).To(BeNil())
				})

				It("returns custom domains with 'http://', excluding the default domain", func() {
					domainNames, err := proj.DomainNamesWithProtocol(db)
					Expect(err).To(BeNil())
					Expect(domainNames).To(Equal([]string{
						"http://foo-bar-express.com",
						"http://foobarexpress.com",
					}))
				})
			})
		})
	})
})
