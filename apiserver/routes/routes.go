package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers/acme"
	"github.com/nitrous-io/rise-server/apiserver/controllers/certs"
	"github.com/nitrous-io/rise-server/apiserver/controllers/deployments"
	"github.com/nitrous-io/rise-server/apiserver/controllers/domains"
	"github.com/nitrous-io/rise-server/apiserver/controllers/hooks"
	"github.com/nitrous-io/rise-server/apiserver/controllers/jsenvvars"
	"github.com/nitrous-io/rise-server/apiserver/controllers/oauth"
	"github.com/nitrous-io/rise-server/apiserver/controllers/ping"
	"github.com/nitrous-io/rise-server/apiserver/controllers/projects"
	"github.com/nitrous-io/rise-server/apiserver/controllers/rawbundles"
	"github.com/nitrous-io/rise-server/apiserver/controllers/repos"
	"github.com/nitrous-io/rise-server/apiserver/controllers/root"
	"github.com/nitrous-io/rise-server/apiserver/controllers/stats"
	"github.com/nitrous-io/rise-server/apiserver/controllers/templates"
	"github.com/nitrous-io/rise-server/apiserver/controllers/users"
	"github.com/nitrous-io/rise-server/apiserver/middleware"
)

func Draw(r *gin.Engine) {
	if gin.Mode() != gin.TestMode {
		r.Use(gin.Logger())
		r.Use(gin.Recovery())
	}

	r.Use(middleware.CORS)

	r.GET("/", root.Root)
	r.GET("/ping", ping.Ping)
	r.POST("/users", users.Create)
	r.POST("/user/confirm", users.Confirm)
	r.POST("/user/confirm/resend", users.ResendConfirmationCode)
	r.POST("/user/password/forgot", users.ForgotPassword)
	r.POST("/user/password/reset", users.ResetPassword)
	r.POST("/oauth/token", oauth.CreateToken)
	r.GET("/admin/stats", stats.Index)

	r.GET("/.well-known/acme-challenge/:token", acme.ChallengeResponse)

	r.POST("/hooks/github/:path", hooks.GitHubPush)

	{ // Routes that require a OAuth Token
		authorized := r.Group("", middleware.RequireToken)
		authorized.DELETE("/oauth/token", oauth.DestroyToken)
		authorized.POST("/projects", projects.Create)
		authorized.GET("/projects", projects.Index)
		authorized.GET("/user", users.Show)
		authorized.PUT("/user", users.Update)
		authorized.GET("/templates", templates.Index)

		{ // Routes that either project owners or collaborators can access
			projCollab := authorized.Group("/projects/:project_name", middleware.RequireProjectCollab)

			projCollab.GET("", projects.Get)
			projCollab.GET("/deployments/:id", deployments.Show)
			projCollab.GET("/deployments", deployments.Index)
			projCollab.GET("repos", repos.Show)
			projCollab.POST("/repos", repos.Link)
			projCollab.DELETE("/repos", repos.Unlink)
			projCollab.GET("/domains", domains.Index)
			projCollab.GET("/collaborators", projects.ListCollaborators)
			projCollab.GET("/domains/:name/cert", certs.Show)
			projCollab.POST("/domains/:name/cert", certs.Create)
			projCollab.POST("/domains/:name/cert/letsencrypt", certs.LetsEncrypt)
			projCollab.DELETE("/domains/:name/cert", certs.Destroy)
			projCollab.GET("/raw_bundles/:bundle_checksum", rawbundles.Get)
			projCollab.GET("/jsenvvars", jsenvvars.Index)

			{ // Routes that lock a project
				lock := projCollab.Group("", middleware.LockProject)
				lock.PUT("", projects.Update)
				lock.POST("/deployments", deployments.Create)
				lock.POST("/domains", domains.Create)
				lock.DELETE("/domains/:name", domains.Destroy)
				lock.POST("/rollback", deployments.Rollback)
				lock.POST("/auth", projects.CreateAuth)
				lock.DELETE("/auth", projects.DeleteAuth)
				lock.PUT("/jsenvvars/add", jsenvvars.Add)
				lock.PUT("/jsenvvars/delete", jsenvvars.Delete)
			}
		}

		{ // Routes that only project owners can access
			projOwner := authorized.Group("/projects/:project_name", middleware.RequireProject)

			projOwner.POST("/collaborators", projects.AddCollaborator)
			projOwner.DELETE("/collaborators/:email", projects.RemoveCollaborator)

			{ // Routes that lock a project
				lock := projOwner.Group("", middleware.LockProject)
				lock.DELETE("", projects.Destroy) // DELETE /projects/:project_name
			}
		}
	}
}
