package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/controllers/oauth"
	"github.com/nitrous-io/rise-server/controllers/ping"
	"github.com/nitrous-io/rise-server/controllers/users"
)

func Draw(r *gin.Engine) {
	r.GET("/ping", ping.Ping)
	r.POST("/users", users.Create)
	r.POST("/user/confirm", users.Confirm)
	r.POST("/oauth/token", oauth.CreateToken)
}
