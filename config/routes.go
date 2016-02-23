package config

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/controllers/ping"
	"github.com/nitrous-io/rise-server/controllers/users"
)

func Routes(r *gin.Engine) {
	r.GET("/ping", ping.Ping)
	r.POST("/users", users.Create)
}
