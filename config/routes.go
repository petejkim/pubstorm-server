package config

import (
	"github.com/gin-gonic/gin"
	"github.com/petejkim/rise-server/controllers/ping"
)

func Routes(r *gin.Engine) {
	r.GET("/ping", ping.Ping)
}
