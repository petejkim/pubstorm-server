package projects_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/collab"
	"github.com/nitrous-io/rise-server/apiserver/models/oauthtoken"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/user"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/tracker"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	"github.com/nitrous-io/rise-server/testhelper/sharedexamples"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Project collaborators", func() {
	var (
		db      *gorm.DB
		s       *httptest.Server
		res     *http.Response
		headers http.Header
		err     error

		u    *user.User
		t    *oauthtoken.OauthToken
		proj *project.Project

		fakeTracker *fake.Tracker
		origTracker tracker.Trackable
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u, _, t = factories.AuthTrio(db)

		headers = http.Header{
			"Authorization": {"Bearer " + t.Token},
		}

		proj = &project.Project{
			Name:   "panda-express",
			UserID: u.ID,
		}
		Expect(db.Create(proj).Error).To(BeNil())

		origTracker = common.Tracker
		fakeTracker = &fake.Tracker{}
		common.Tracker = fakeTracker
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()

		common.Tracker = origTracker
	})

	Describe("GET /projects/collaborators", func() {
		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/projects/panda-express/collaborators", nil, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when the project has collaborators", func() {
			var (
				u2 *user.User
				u3 *user.User
			)

			BeforeEach(func() {
				u2 = factories.User(db)
				u3 = factories.User(db)
				factories.Collab(db, proj, u2)
				factories.Collab(db, proj, u3)
				factories.Collab(db, nil, nil) // another project
			})

			It("returns 200 OK with the project's collaborators", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"collaborators": [
						{
							"email": "%s"
						},
						{
							"email": "%s"
						}
					]
				}`, u2.Email, u3.Email)))
			})
		})

		Context("when there are no collaborators", func() {
			It("returns 200 OK with no collaborators", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"collaborators": []
				}`))
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)

		sharedexamples.ItRequiresProjectCollab(func() (*gorm.DB, *user.User, *project.Project) {
			return db, u, proj
		}, func() *http.Response {
			doRequest()
			return res
		}, nil)
	})

	Describe("POST /projects/collaborators", func() {
		doRequest := func(params url.Values) {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/projects/panda-express/collaborators", params, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when using an email that does not exist", func() {
			It("returns 422 unprocessable entity", func() {
				doRequest(url.Values{"email": {"fakestevejobs@apple.com"}})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_params",
					"error_description": "email is not found"
				}`))
			})
		})

		Context("when adding the owner of the project as a collaborator", func() {
			It("returns 422 unprocessable entity", func() {
				doRequest(url.Values{"email": {u.Email}})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_request",
					"error_description": "the owner of a project cannot be added as a collaborator"
				}`))
			})
		})

		Context("when adding an existing collaborator", func() {
			var anotherU *user.User

			BeforeEach(func() {
				anotherU = factories.User(db)
				factories.Collab(db, proj, anotherU)
			})

			It("returns 409 Conflict", func() {
				doRequest(url.Values{"email": {anotherU.Email}})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusConflict))
				Expect(b.String()).To(MatchJSON(`{
					"error": "already_exists",
					"error_description": "user is already a collaborator"
				}`))
			})
		})

		Context("when adding a valid user as a collaborator", func() {
			var anotherU *user.User

			BeforeEach(func() {
				anotherU = factories.User(db)
			})

			It("returns 201 Created and adds user as a collaborator to the project", func() {
				doRequest(url.Values{"email": {anotherU.Email}})

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusCreated))
				Expect(b.String()).To(MatchJSON(`{
					"added": true
				}`))

				cols := []collab.Collab{}
				err = db.Model(collab.Collab{}).Where("project_id = ?", proj.ID).Find(&cols).Error
				Expect(err).To(BeNil())
				Expect(len(cols)).To(Equal(1))
				Expect(cols[0].UserID).To(Equal(anotherU.ID))
				Expect(cols[0].ProjectID).To(Equal(proj.ID))
			})

			It("tracks an 'Added Collaborator' event", func() {
				doRequest(url.Values{"email": {anotherU.Email}})

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Added Collaborator"))
				Expect(trackCall.Arguments[2]).To(Equal(""))

				t := trackCall.Arguments[3]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal("panda-express"))
				Expect(props["collabEmail"]).To(Equal(anotherU.Email))

				c := trackCall.Arguments[4]
				context, ok := c.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(context["ip"]).ToNot(BeNil())
				Expect(context["user_agent"]).ToNot(BeNil())

				Expect(trackCall.ReturnValues[0]).To(BeNil())
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest(url.Values{"email": {"foo"}})
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest(url.Values{"email": {"foo"}})
			return res
		}, nil)
	})

	Describe("DELETE /projects/collaborators/:email", func() {
		doRequest := func(email string) {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("DELETE",
				fmt.Sprintf("%s/projects/panda-express/collaborators/%s", s.URL, email),
				nil, headers, nil)
			Expect(err).To(BeNil())
		}

		Context("when user does not exist", func() {
			It("returns 404 Not Found", func() {
				doRequest("non-existent@void.io")

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(b.String()).To(MatchJSON(`{
					"error": "not_found",
					"error_description": "email is not found"
				}`))
			})
		})

		Context("when removing a user who is not already a collaborator", func() {
			It("returns 200 OK", func() {
				anotherU := factories.User(db)

				doRequest(anotherU.Email)

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"removed": true
				}`))
			})
		})

		Context("when removing a user who is a collaborator", func() {
			var u2 *user.User

			BeforeEach(func() {
				u2 = factories.User(db)
				factories.Collab(db, proj, u2)
			})

			It("returns 200 OK and removes the user as a collaborator", func() {
				// Add another collaborator to test that it doesn't get removed.
				u3 := factories.User(db)
				factories.Collab(db, proj, u3)

				doRequest(u2.Email)

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"removed": true
				}`))

				cols := []collab.Collab{}
				err = db.Model(collab.Collab{}).Where("project_id = ?", proj.ID).Find(&cols).Error
				Expect(err).To(BeNil())
				Expect(len(cols)).To(Equal(1))
				Expect(cols[0].UserID).To(Equal(u3.ID))
				Expect(cols[0].ProjectID).To(Equal(proj.ID))
			})

			It("tracks a 'Removed Collaborator' event", func() {
				doRequest(u2.Email)

				trackCall := fakeTracker.TrackCalls.NthCall(1)
				Expect(trackCall).NotTo(BeNil())
				Expect(trackCall.Arguments[0]).To(Equal(fmt.Sprintf("%d", u.ID)))
				Expect(trackCall.Arguments[1]).To(Equal("Removed Collaborator"))
				Expect(trackCall.Arguments[2]).To(Equal(""))

				t := trackCall.Arguments[3]
				props, ok := t.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(props["projectName"]).To(Equal("panda-express"))
				Expect(props["collabEmail"]).To(Equal(u2.Email))

				c := trackCall.Arguments[4]
				context, ok := c.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(context["ip"]).ToNot(BeNil())
				Expect(context["user_agent"]).ToNot(BeNil())

				Expect(trackCall.ReturnValues[0]).To(BeNil())
			})
		})

		sharedexamples.ItRequiresAuthentication(func() (*gorm.DB, *user.User, *http.Header) {
			return db, u, &headers
		}, func() *http.Response {
			doRequest("non-existent@void.io")
			return res
		}, nil)

		sharedexamples.ItRequiresProject(func() (*gorm.DB, *project.Project) {
			return db, proj
		}, func() *http.Response {
			doRequest("non-existent@void.io")
			return res
		}, nil)
	})
})
