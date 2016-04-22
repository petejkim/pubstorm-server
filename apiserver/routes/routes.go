package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers/deployments"
	"github.com/nitrous-io/rise-server/apiserver/controllers/domains"
	"github.com/nitrous-io/rise-server/apiserver/controllers/oauth"
	"github.com/nitrous-io/rise-server/apiserver/controllers/ping"
	"github.com/nitrous-io/rise-server/apiserver/controllers/projects"
	"github.com/nitrous-io/rise-server/apiserver/controllers/users"
	"github.com/nitrous-io/rise-server/apiserver/middleware"
)

func Draw(r *gin.Engine) {
	if gin.Mode() != gin.TestMode {
		r.Use(gin.Logger())
		r.Use(gin.Recovery())
	}

	r.GET("/ping", ping.Ping)
	r.POST("/users", users.Create)
	r.POST("/user/confirm", users.Confirm)
	r.POST("/user/confirm/resend", users.ResendConfirmationCode)
	r.POST("/user/password/forgot", users.ForgotPassword)
	r.POST("/user/password/reset", users.ResetPassword)
	r.POST("/oauth/token", oauth.CreateToken)

	{ // Routes that require a OAuth Token
		authorized := r.Group("", middleware.RequireToken)
		authorized.DELETE("/oauth/token", oauth.DestroyToken)
		authorized.POST("/projects", projects.Create)
		authorized.GET("/projects", projects.Index)
		authorized.PUT("/user", users.Update)

		{ // Routes that either project owners or collaborators can access
			projCollab := authorized.Group("/projects/:project_name", middleware.RequireProjectCollab)

			projCollab.GET("", projects.Get)
			projCollab.GET("/deployments/:id", deployments.Show)
			projCollab.GET("/domains", domains.Index)
			projCollab.GET("/collaborators", projects.ListCollaborators)

			{ // Routes that lock a project
				lock := projCollab.Group("", middleware.LockProject)
				lock.PUT("", projects.Update)
				lock.POST("/deployments", deployments.Create)
				lock.POST("/domains", domains.Create)
				lock.DELETE("/domains/:name", domains.Destroy)
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
