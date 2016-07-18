package pushd_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/push"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
	"github.com/nitrous-io/rise-server/pkg/filetransfer"
	"github.com/nitrous-io/rise-server/pkg/mqconn"
	"github.com/nitrous-io/rise-server/pushd/pushd"
	"github.com/nitrous-io/rise-server/shared/queues"
	"github.com/nitrous-io/rise-server/shared/s3client"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"
	"github.com/nitrous-io/rise-server/testhelper/fake"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/streadway/amqp"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pushd")
}

var _ = Describe("Pushd", func() {
	var (
		err error
		db  *gorm.DB
		mq  *amqp.Connection

		proj *project.Project
		depl *deployment.Deployment
		rp   *repo.Repo
		pu   *push.Push

		githubAPIServer   *ghttp.Server
		origGitHubAPIHost string

		// These values can be changed in tests to test cases other than the
		// "happy path".
		contentsStatusCode int
		contentsBody       string
		archiveStatusCode  int
		archiveBody        string

		fakeS3 *fake.S3
		origS3 filetransfer.FileTransfer
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		mq, err = mqconn.MQ()
		Expect(err).To(BeNil())
		testhelper.DeleteQueue(mq, queues.All...)

		contentsStatusCode = http.StatusOK
		contentsBody = `{ "name": "", "path": "./build" }`
		archiveStatusCode = http.StatusOK

		tarballFixture, err := ioutil.ReadFile("../../testhelper/fixtures/github-repo-archive.tar.gz")
		Expect(err).To(BeNil())
		archiveBody = string(tarballFixture)

		githubAPIServer = ghttp.NewServer()
		githubAPIServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/repos/chuyeow/chuyeow.github.io/contents/pubstorm.json", "ref=deafcafe1e9e5ae2ff1314666e366cbc7260dc"),
				ghttp.RespondWithPtr(&contentsStatusCode, &contentsBody),
			),
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/repos/chuyeow/chuyeow.github.io/tarball/deafcafe1e9e5ae2ff1314666e366cbc7260dc"),
				ghttp.RespondWith(http.StatusFound, "", http.Header{"Location": {githubAPIServer.URL() + "/chuyeow/chuyeow.github.io/legacy.tar.gz/deafcafe1e9e5ae2ff1314666e366cbc7260dc"}}),
			),
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/chuyeow/chuyeow.github.io/legacy.tar.gz/deafcafe1e9e5ae2ff1314666e366cbc7260dc"),
				ghttp.RespondWithPtr(&archiveStatusCode, &archiveBody, http.Header{"Content-Type": {"application/x-gzip"}}),
			),
		)

		origGitHubAPIHost = common.GitHubAPIHost
		common.GitHubAPIHost = githubAPIServer.URL()

		// Replace "https://api.github.com" in GitHub payload with URL of fake
		// server.
		fakePayload := strings.Replace(string(ghPushPayload), "https://api.github.com", githubAPIServer.URL(), -1)

		u := factories.User(db)
		proj = factories.Project(db, u)
		depl = factories.DeploymentWithAttrs(db, proj, u, deployment.Deployment{
			State: deployment.StatePendingUpload,
		})
		rp = &repo.Repo{
			ProjectID: proj.ID,
			UserID:    u.ID,
			URI:       "https://github.com/chuyeow/chuyeow.github.io",
			Branch:    "release",
		}
		Expect(db.Create(rp).Error).To(BeNil())
		pu = &push.Push{
			RepoID:       rp.ID,
			DeploymentID: depl.ID,
			Ref:          "deafcafe1e9e5ae2ff1314666e366cbc7260dc",
			Payload:      fakePayload,
		}
		Expect(db.Create(pu).Error).To(BeNil())

		origS3 = s3client.S3
		fakeS3 = &fake.S3{}
		pushd.S3 = fakeS3
	})

	AfterEach(func() {
		common.GitHubAPIHost = origGitHubAPIHost
		pushd.S3 = origS3
	})

	It("downloads the GitHub repository archive, and uploads only the files in the project path specified in the repository's pubstorm.json file to S3", func() {
		err := pushd.Work([]byte(fmt.Sprintf(`{
				"push_id": %d
			}`, pu.ID)))
		Expect(err).To(BeNil())

		Expect(githubAPIServer.ReceivedRequests()).To(HaveLen(3))

		Expect(fakeS3.UploadCalls.Count()).To(Equal(1))
		uploadCall := fakeS3.UploadCalls.NthCall(1)
		Expect(uploadCall).NotTo(BeNil())
		Expect(uploadCall.Arguments[0]).To(Equal(s3client.BucketRegion))
		Expect(uploadCall.Arguments[1]).To(Equal(s3client.BucketName))
		Expect(uploadCall.Arguments[2]).To(Equal(fmt.Sprintf("deployments/%s/raw-bundle.tar.gz", depl.PrefixID())))
		Expect(uploadCall.Arguments[4]).To(Equal(""))
		Expect(uploadCall.Arguments[5]).To(Equal("private"))
		Expect(uploadCall.ReturnValues[0]).To(BeNil())

		// Verify that uploaded files are the ones in the project path.
		filenames := []string{}
		uploadedContent, ok := uploadCall.SideEffects["uploaded_content"].([]byte)
		Expect(ok).To(BeTrue())
		buf := bytes.NewBuffer(uploadedContent)
		gr, err := gzip.NewReader(buf)
		Expect(err).To(BeNil())
		defer gr.Close()
		tr := tar.NewReader(gr)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			Expect(err).To(BeNil())

			if hdr.FileInfo().IsDir() {
				continue
			}

			filenames = append(filenames, hdr.Name)
		}

		// See testhelper/fixtures/github-repo-archive/ directory or unarchive the
		// testhelper/fixtures/github-repo-archive.tar.gz file.
		Expect(filenames).To(ConsistOf("index.html", "css/app.css"))
	})

	It("enqueues a build job", func() {
		Expect(proj.SkipBuild).To(BeFalse())

		err := pushd.Work([]byte(fmt.Sprintf(`{
			"push_id": %d
		}`, pu.ID)))
		Expect(err).To(BeNil())

		d := testhelper.ConsumeQueue(mq, queues.Build)
		Expect(d).NotTo(BeNil())
		Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
			"deployment_id": %d
		}`, depl.ID)))
	})

	Context("when project's skip_build column is true", func() {
		BeforeEach(func() {
			proj.SkipBuild = true
			Expect(db.Save(proj).Error).To(BeNil())
		})

		It("enqueues a deploy job", func() {
			err := pushd.Work([]byte(fmt.Sprintf(`{
				"push_id": %d
			}`, pu.ID)))
			Expect(err).To(BeNil())

			d := testhelper.ConsumeQueue(mq, queues.Deploy)
			Expect(d).NotTo(BeNil())
			Expect(d.Body).To(MatchJSON(fmt.Sprintf(`{
				"deployment_id": %d,
				"skip_webroot_upload": false,
				"skip_invalidation": false,
				"use_raw_bundle": true
			}`, depl.ID)))
		})
	})

	Context("when the repository does not contain a pubstorm.json", func() {
		BeforeEach(func() {
			contentsStatusCode = http.StatusNotFound
		})

		It("returns an error", func() {
			err := pushd.Work([]byte(fmt.Sprintf(`{
				"push_id": %d
			}`, pu.ID)))
			Expect(err).To(Equal(pushd.ErrProjectConfigNotFound))

			err = db.First(depl, pu.DeploymentID).Error
			Expect(err).To(BeNil())

			Expect(*depl.ErrorMessage).To(Equal("Your GitHub repository does not contain a pubstorm.json file, aborting. Please check in the pubstorm.json file in the root of your repository."))
			Expect(depl.State).To(Equal(deployment.StateDeployFailed))
		})
	})

	Context("when the repository's pubstorm.json is in an invalid format", func() {
		BeforeEach(func() {
			contentsBody = `{ "invalid json" }`
		})

		It("returns an error", func() {
			err := pushd.Work([]byte(fmt.Sprintf(`{
				"push_id": %d
			}`, pu.ID)))
			Expect(err).To(Equal(pushd.ErrProjectConfigInvalidFormat))

			err = db.First(depl, pu.DeploymentID).Error
			Expect(err).To(BeNil())

			Expect(*depl.ErrorMessage).To(Equal("Your repository's pubstorm.json is in an invalid format, aborting."))
			Expect(depl.State).To(Equal(deployment.StateDeployFailed))
		})
	})
})

var ghPushPayload = []byte(`{
	"ref": "refs/heads/master",
	"before": "a0fbcc76e4b2453c35261419208e2f72c98b010f",
	"after": "deafcafe1e9e5ae2ff1314666e366cbc7260dc",
	"created": false,
	"deleted": false,
	"forced": false,
	"base_ref": null,
	"compare": "https://github.com/chuyeow/chuyeow.github.io/compare/a0fbcc76e4b2...5e908dc1f01e",
	"commits": [
		{
			"id": "deafcafe1e9e5ae2ff1314666e366cbc7260dc",
			"tree_id": "5f55125a021ca2e42eb781286f7ba1d6ab6a8056",
			"distinct": true,
			"message": "Push test.",
			"timestamp": "2016-07-14T07:55:01+08:00",
			"url": "https://github.com/chuyeow/chuyeow.github.io/commit/deafcafe1e9e5ae2ff1314666e366cbc7260dc",
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
		"id": "deafcafe1e9e5ae2ff1314666e366cbc7260dc",
		"tree_id": "5f55125a021ca2e42eb781286f7ba1d6ab6a8056",
		"distinct": true,
		"message": "Push test.",
		"timestamp": "2016-07-14T07:55:01+08:00",
		"url": "https://github.com/chuyeow/chuyeow.github.io/commit/deafcafe1e9e5ae2ff1314666e366cbc7260dc",
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
