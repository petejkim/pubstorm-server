package hooks_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/push"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "github")
}

var _ = Describe("GitHub", func() {
	var (
		db *gorm.DB
		mq *amqp.Connection

		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())
		testhelper.DeleteQueue(mq, queues.All...)
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("POST /hooks/github/:path", func() {
		var (
			reqBody io.Reader
			headers http.Header
			reqPath string

			proj *project.Project
			rp   *repo.Repo
		)

		BeforeEach(func() {
			reqBody = bytes.NewBuffer(ghPushPayload)
			headers = http.Header{}
			headers.Set("X-GitHub-Event", "push")

			proj = factories.Project(db, nil)
			rp = &repo.Repo{
				ProjectID:     proj.ID,
				UserID:        proj.UserID,
				URI:           "https://github.com/chuyeow/chuyeow.github.io.git",
				Branch:        "master",
				WebhookPath:   "defacedbeeffece5",
				WebhookSecret: "secrud1985",
			}
			Expect(db.Create(rp).Error).To(BeNil())

			reqPath = rp.WebhookPath

			// Set a "X-Hub-Signature" request header with HMAC of request body,
			// using repo secret as the key.
			mac := hmac.New(sha1.New, []byte(rp.WebhookSecret))
			mac.Write(ghPushPayload)
			headers.Set("X-Hub-Signature", "sha1="+hex.EncodeToString(mac.Sum(nil)))
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			req, err := http.NewRequest("POST", s.URL+"/hooks/github/"+reqPath, reqBody)
			Expect(err).To(BeNil())
			req.Header.Set("Content-Type", "application/json")
			if headers != nil {
				for k, v := range headers {
					for _, h := range v {
						req.Header.Add(k, h)
					}
				}
			}
			res, err = http.DefaultClient.Do(req)
			Expect(err).To(BeNil())
		}

		It("responds with HTTP 200 OK", func() {
			doRequest()

			Expect(res.StatusCode).To(Equal(http.StatusOK))

			b := &bytes.Buffer{}
			_, err = b.ReadFrom(res.Body)

			Expect(b.String()).To(Equal("A deployment has been initiated by this push."))
		})

		It("creates a deployment record and an associated push record", func() {
			doRequest()

			depl := &deployment.Deployment{}
			db.Last(depl)

			Expect(depl).NotTo(BeNil())
			Expect(depl.ProjectID).To(Equal(proj.ID))
			Expect(depl.UserID).To(Equal(rp.UserID))
			Expect(depl.State).To(Equal(deployment.StatePendingUpload))
			Expect(depl.Prefix).NotTo(HaveLen(0))
			Expect(depl.Version).To(Equal(int64(1)))
			Expect(depl.RawBundleID).To(BeNil())
			Expect(depl.JsEnvVars).To(Equal([]byte("{}")))

			push := &push.Push{}
			db.Last(push)

			Expect(push).NotTo(BeNil())
			Expect(push.RepoID).To(Equal(rp.ID))
			Expect(push.DeploymentID).To(Equal(depl.ID))
			Expect(push.Ref).To(Equal("5e908dc1f01e9e5ae2ff1314666e366cbc7260dc"))
			Expect(push.Payload).To(Equal(string(ghPushPayload)))
			Expect(push.ProcessedAt).To(BeNil())
		})

		It("enqueues a push job", func() {
			doRequest()

			push := &push.Push{}
			db.Last(push)

			d := testhelper.ConsumeQueue(mq, queues.Push)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"push_id": %d
			}`, push.ID)))
		})

		Context("when request body is empty (i.e. GitHub sends a request with empty body)", func() {
			BeforeEach(func() {
				reqBody = nil
			})

			It("responds with HTTP 202 Accepted but returns an error message", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(Equal("Payload is empty or is in an unexpected format."))
			})
		})

		Context("when request body is not valid JSON", func() {
			BeforeEach(func() {
				reqBody = bytes.NewBufferString("plain olde string")
			})

			It("responds with HTTP 202 Accepted but returns an error message", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(Equal("Payload is empty or is in an unexpected format."))
			})
		})

		Context("when the X-GitHub-Event header is not equal to 'push'", func() {
			BeforeEach(func() {
				headers.Set("X-GitHub-Event", "create")
			})

			It("responds with HTTP 202 Accepted but returns an error message", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(Equal("Only push events are processed."))
			})
		})

		Context("when the webhook path does not match a repo in the DB", func() {
			BeforeEach(func() {
				reqPath = "some-other-path.aspx"
			})

			It("responds with HTTP 202 Accepted but returns an error message", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(Equal("Payload URL unknown. Are you using the correct Webhook URL for your PubStorm project?"))
			})
		})

		Context("when branch in payload is not the same as that of the repo", func() {
			BeforeEach(func() {
				rp.Branch = "release"
				Expect(db.Save(rp).Error).To(BeNil())
			})

			It("responds with HTTP 202 Accepted but returns an error message", func() {
				doRequest()

				Expect(res.StatusCode).To(Equal(http.StatusAccepted))

				b := &bytes.Buffer{}
				_, err = b.ReadFrom(res.Body)

				Expect(b.String()).To(Equal(`Payload is not for the "release" branch, aborting.`))
			})
		})

		Context("when a webhook secret is set in the repo record", func() {
			Context("when the X-Hub-Signature header is empty", func() {
				BeforeEach(func() {
					headers.Del("X-Hub-Signature")
				})

				It("responds with HTTP 202 Accepted but returns an error message", func() {
					doRequest()

					Expect(res.StatusCode).To(Equal(http.StatusAccepted))

					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(b.String()).To(Equal("Webhook secret empty. Please enter the Webhook Secret into the Secret textbox of your webhook on GitHub."))
				})
			})

			Context("when the X-Hub-Signature header doesn't match the hex-encoded HMAC signature of the payload", func() {
				BeforeEach(func() {
					headers.Set("X-Hub-Signature", "this-definitely-wont-match")
				})

				It("responds with HTTP 202 Accepted but returns an error message", func() {
					doRequest()

					Expect(res.StatusCode).To(Equal(http.StatusAccepted))

					b := &bytes.Buffer{}
					_, err = b.ReadFrom(res.Body)

					Expect(b.String()).To(Equal("Webhook secret incorrect. Please enter the Webhook Secret into the Secret textbox of your webhook on GitHub."))
				})
			})
		})
	})
})

var ghPushPayload = []byte(`{
  "ref": "refs/heads/master",
  "before": "a0fbcc76e4b2453c35261419208e2f72c98b010f",
  "after": "5e908dc1f01e9e5ae2ff1314666e366cbc7260dc",
  "created": false,
  "deleted": false,
  "forced": false,
  "base_ref": null,
  "compare": "https://github.com/chuyeow/chuyeow.github.io/compare/a0fbcc76e4b2...5e908dc1f01e",
  "commits": [
    {
      "id": "5e908dc1f01e9e5ae2ff1314666e366cbc7260dc",
      "tree_id": "5f55125a021ca2e42eb781286f7ba1d6ab6a8056",
      "distinct": true,
      "message": "Push test.",
      "timestamp": "2016-07-14T07:55:01+08:00",
      "url": "https://github.com/chuyeow/chuyeow.github.io/commit/5e908dc1f01e9e5ae2ff1314666e366cbc7260dc",
      "author": {
        "name": "Cheah Chu Yeow",
        "email": "chuyeow@gmail.com",
        "username": "chuyeow"
      },
      "committer": {
        "name": "GitHub",
        "email": "noreply@github.com",
        "username": "web-flow"
      },
      "added": [

      ],
      "removed": [

      ],
      "modified": [
        "index.html"
      ]
    }
  ],
  "head_commit": {
    "id": "5e908dc1f01e9e5ae2ff1314666e366cbc7260dc",
    "tree_id": "5f55125a021ca2e42eb781286f7ba1d6ab6a8056",
    "distinct": true,
    "message": "Push test.",
    "timestamp": "2016-07-14T07:55:01+08:00",
    "url": "https://github.com/chuyeow/chuyeow.github.io/commit/5e908dc1f01e9e5ae2ff1314666e366cbc7260dc",
    "author": {
      "name": "Cheah Chu Yeow",
      "email": "chuyeow@gmail.com",
      "username": "chuyeow"
    },
    "committer": {
      "name": "GitHub",
      "email": "noreply@github.com",
      "username": "web-flow"
    },
    "added": [

    ],
    "removed": [

    ],
    "modified": [
      "index.html"
    ]
  },
  "repository": {
    "id": 63208048,
    "name": "chuyeow.github.io",
    "full_name": "chuyeow/chuyeow.github.io",
    "owner": {
      "name": "chuyeow",
      "email": "chuyeow@gmail.com"
    },
    "private": false,
    "html_url": "https://github.com/chuyeow/chuyeow.github.io",
    "description": null,
    "fork": false,
    "url": "https://github.com/chuyeow/chuyeow.github.io",
    "forks_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/forks",
    "keys_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/keys{/key_id}",
    "collaborators_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/collaborators{/collaborator}",
    "teams_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/teams",
    "hooks_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/hooks",
    "issue_events_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/issues/events{/number}",
    "events_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/events",
    "assignees_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/assignees{/user}",
    "branches_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/branches{/branch}",
    "tags_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/tags",
    "blobs_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/git/blobs{/sha}",
    "git_tags_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/git/tags{/sha}",
    "git_refs_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/git/refs{/sha}",
    "trees_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/git/trees{/sha}",
    "statuses_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/statuses/{sha}",
    "languages_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/languages",
    "stargazers_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/stargazers",
    "contributors_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/contributors",
    "subscribers_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/subscribers",
    "subscription_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/subscription",
    "commits_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/commits{/sha}",
    "git_commits_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/git/commits{/sha}",
    "comments_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/comments{/number}",
    "issue_comment_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/issues/comments{/number}",
    "contents_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/contents/{+path}",
    "compare_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/compare/{base}...{head}",
    "merges_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/merges",
    "archive_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/{archive_format}{/ref}",
    "downloads_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/downloads",
    "issues_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/issues{/number}",
    "pulls_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/pulls{/number}",
    "milestones_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/milestones{/number}",
    "notifications_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/notifications{?since,all,participating}",
    "labels_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/labels{/name}",
    "releases_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/releases{/id}",
    "deployments_url": "https://api.github.com/repos/chuyeow/chuyeow.github.io/deployments",
    "created_at": 1468377399,
    "updated_at": "2016-07-13T03:01:06Z",
    "pushed_at": 1468454101,
    "git_url": "git://github.com/chuyeow/chuyeow.github.io.git",
    "ssh_url": "git@github.com:chuyeow/chuyeow.github.io.git",
    "clone_url": "https://github.com/chuyeow/chuyeow.github.io.git",
    "svn_url": "https://github.com/chuyeow/chuyeow.github.io",
    "homepage": null,
    "size": 0,
    "stargazers_count": 0,
    "watchers_count": 0,
    "language": "HTML",
    "has_issues": true,
    "has_downloads": true,
    "has_wiki": true,
    "has_pages": true,
    "forks_count": 0,
    "mirror_url": null,
    "open_issues_count": 0,
    "forks": 0,
    "open_issues": 0,
    "watchers": 0,
    "default_branch": "master",
    "stargazers": 0,
    "master_branch": "master"
  },
  "pusher": {
    "name": "chuyeow",
    "email": "chuyeow@gmail.com"
  },
  "sender": {
    "login": "chuyeow",
    "id": 213,
    "avatar_url": "https://avatars.githubusercontent.com/u/213?v=3",
    "gravatar_id": "",
    "url": "https://api.github.com/users/chuyeow",
    "html_url": "https://github.com/chuyeow",
    "followers_url": "https://api.github.com/users/chuyeow/followers",
    "following_url": "https://api.github.com/users/chuyeow/following{/other_user}",
    "gists_url": "https://api.github.com/users/chuyeow/gists{/gist_id}",
    "starred_url": "https://api.github.com/users/chuyeow/starred{/owner}{/repo}",
    "subscriptions_url": "https://api.github.com/users/chuyeow/subscriptions",
    "organizations_url": "https://api.github.com/users/chuyeow/orgs",
    "repos_url": "https://api.github.com/users/chuyeow/repos",
    "events_url": "https://api.github.com/users/chuyeow/events{/privacy}",
    "received_events_url": "https://api.github.com/users/chuyeow/received_events",
    "type": "User",
    "site_admin": false
  }
}`)
