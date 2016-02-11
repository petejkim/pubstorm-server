package server

import (
	"github.com/gin-gonic/gin"
	"github.com/petejkim/rise-server/server/ping"
)

func configureRoutes(r *gin.Engine) {
	r.GET("/ping", ping.Ping)
}
