package server

import (
	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/routes"
)

func New() *gin.Engine {
	r := gin.New()
	routes.Draw(r)
	return r
}
