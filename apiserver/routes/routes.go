package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers/deployments"
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
	r.POST("/oauth/token", oauth.CreateToken)

	{
		rr := r.Group("/", middleware.RequireToken)
		rr.DELETE("/oauth/token", oauth.DestroyToken)
		rr.POST("/projects", projects.Create)
		rr.POST("/projects/:name/deployments", deployments.Create)
	}
}