package server

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/config"
)

func New() *gin.Engine {
	r := gin.New()

	config.Middleware(r)
	config.Routes(r)

	return r
}
