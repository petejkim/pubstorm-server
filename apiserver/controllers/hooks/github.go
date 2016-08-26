package hooks

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/models/push"
	"github.com/nitrous-io/rise-server/apiserver/models/repo"
	"github.com/nitrous-io/rise-server/pkg/githubapi"
	"github.com/nitrous-io/rise-server/pkg/job"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/queues"
)

func GitHubPush(c *gin.Context) {
	// We slurp in the entire request body here (instead of using c.BindJSON())
	// because we need it as a string for HMAC verification later.
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusAccepted, "Failed to read payload.")
		return
	}

	// See https://developer.github.com/webhooks/#payloads for details of what
	// GitHub POSTs to this endpoint.
	var pl githubapi.PushPayload
	if err := json.Unmarshal(body, &pl); err != nil {
		log.Errorf("failed to unmarshal JSON payload from GitHub, err: %v", err)
		c.String(http.StatusAccepted, "Payload is empty or is in an unexpected format.")
		return
	}

	if c.Request.Header.Get("X-GitHub-Event") != "push" {
		// GitHub recommends HTTP 201/202 to ack receipt of payload even if we're
		// not processing it. See https://developer.github.com/guides/best-practices-for-integrators/#use-appropriate-http-status-codes-when-responding-to-github.
		c.String(http.StatusAccepted, "Only push events are processed.")
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	path := c.Param("path")
	var rp repo.Repo
	if err := db.Where("webhook_path = ?", path).First(&rp).Error; err != nil {
		c.String(http.StatusAccepted, "Payload URL unknown. Are you using the correct Webhook URL for your PubStorm project?")
		return
	}

	// We do not verify that git/ssh/clone url matches the saved repo.URI in the
	// db, instead relying on the webhook path + secret to "authenticate".

	if rp.Branch != pl.Branch() {
		c.String(http.StatusAccepted, "Payload is not for the %q branch, aborting.", rp.Branch)
		return
	}

	if rp.WebhookSecret != "" {
		// The X-Hub-Signature contains the HMAC hex digest of the payload if the
		// webhook's secret is non-empty (on GitHub).
		sig := c.Request.Header.Get("X-Hub-Signature")
		if sig == "" {
			c.String(http.StatusAccepted, "Webhook secret empty. Please enter the Webhook Secret into the Secret textbox of your webhook on GitHub.")
			return
		}

		mac := hmac.New(sha1.New, []byte(rp.WebhookSecret))
		mac.Write(body)
		// E.g sha1=7da1a65eadb87f7df30cc12131d3ff0151570204.
		expectedSig := "sha1=" + hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
			c.String(http.StatusAccepted, "Webhook secret incorrect. Please enter the Webhook Secret into the Secret textbox of your webhook on GitHub.")
			return
		}
	}

	unexpectedErr := func(err error) {
		log.Errorf("GitHubPush: unexpected error: %v", err)
		c.String(http.StatusAccepted, "An unexpected error has occurred. If this problem persists, please contact PubStorm support.")
	}

	tx := db.Begin()
	if err := tx.Error; err != nil {
		unexpectedErr(err)
		return
	}
	defer tx.Rollback()

	var proj project.Project
	if err := tx.First(&proj, rp.ProjectID).Error; err != nil {
		unexpectedErr(err)
		return
	}

	// TODO We should record more metadata:
	// E.g. "Triggered by GitHub push by @chuyeow. Changes: https://github.com/PubStorm/pubstorm-www/compare/a0fbcc76e4b2...5e908dc1f01e."
	depl := &deployment.Deployment{
		ProjectID: rp.ProjectID,
		UserID:    rp.UserID,
	}

	// Get JS environment variables from previous deployment.
	if proj.ActiveDeploymentID != nil {
		var prev deployment.Deployment
		if err := tx.Where("id = ?", proj.ActiveDeploymentID).First(&prev).Error; err != nil {
			unexpectedErr(err)
			return
		}

		depl.JsEnvVars = prev.JsEnvVars
	}

	ver, err := proj.NextVersion(tx)
	if err != nil {
		unexpectedErr(err)
		return
	}

	depl.Version = ver
	if err := tx.Create(depl).Error; err != nil {
		unexpectedErr(err)
		return
	}

	pu := &push.Push{
		DeploymentID: depl.ID,
		RepoID:       rp.ID,
		Ref:          pl.After,
		Payload:      string(body),
	}
	if err := tx.Create(pu).Error; err != nil {
		unexpectedErr(err)
		return
	}

	if err := tx.Commit().Error; err != nil {
		unexpectedErr(err)
		return
	}

	jb, err := job.NewWithJSON(queues.Push, &messages.PushJobData{
		PushID: pu.ID,
	})
	if err != nil {
		unexpectedErr(err)
		return
	}
	if err := jb.Enqueue(); err != nil {
		unexpectedErr(err)
		return
	}

	{
		// Track event, attributing it to the user who setup the GitHub repo
		// integration.
		var (
			event = "Initiated Project Deployment"
			props = map[string]interface{}{
				"projectName":       proj.Name,
				"deploymentId":      depl.ID,
				"deploymentPrefix":  depl.Prefix,
				"deploymentVersion": depl.Version,
				"source":            "GitHub push",
			}
			context = map[string]interface{}{
				"ip":         common.GetIP(c.Request),
				"user_agent": c.Request.UserAgent(),
			}
		)
		if err := common.Track(strconv.Itoa(int(rp.UserID)), event, "", props, context); err != nil {
			log.Errorf("failed to track %q event for user ID %d, err: %v",
				event, rp.UserID, err)
		}
	}

	c.String(http.StatusOK, "A deployment has been initiated by this push.")
}
