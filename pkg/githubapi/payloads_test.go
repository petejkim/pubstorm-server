package githubapi_test

import (
	"encoding/json"
	"testing"

	"github.com/nitrous-io/rise-server/pkg/githubapi"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "githubapi")
}

var _ = Describe("githubapi", func() {
	Describe("PushPayload", func() {
		It("can be used to unmarshal a GitHub webhook payload", func() {
			var pl githubapi.PushPayload
			err := json.Unmarshal(sampleGitHubPushPayload, &pl)
			Expect(err).To(BeNil())

			Expect(pl.Ref).To(Equal("refs/heads/master"))
			Expect(pl.After).To(Equal("5e908dc1f01e9e5ae2ff1314666e366cbc7260dc"))
			Expect(pl.Forced).To(Equal(false))
			Expect(pl.CompareURL).To(Equal("https://github.com/chuyeow/chuyeow.github.io/compare/a0fbcc76e4b2...5e908dc1f01e"))
			Expect(pl.Repository).NotTo(BeZero())
			Expect(pl.Repository.FullName).To(Equal("chuyeow/chuyeow.github.io"))
			Expect(pl.Repository.ArchiveURL).To(Equal("https://api.github.com/repos/chuyeow/chuyeow.github.io/{archive_format}{/ref}"))
			Expect(pl.Pusher).NotTo(BeZero())
			Expect(pl.Pusher.Name).To(Equal("chuyeow"))
		})

		Describe("Branch()", func() {
			It("returns the branch", func() {
				var pl githubapi.PushPayload
				err := json.Unmarshal(sampleGitHubPushPayload, &pl)
				Expect(err).To(BeNil())

				Expect(pl.Branch()).To(Equal("master"))
			})
		})
	})
})

var sampleGitHubPushPayload = []byte(`{
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
