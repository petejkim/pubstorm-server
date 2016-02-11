package server

import "github.com/gin-gonic/gin"

func New() *gin.Engine {
	r := gin.New()

	configureMiddleware(r)
	configureRoutes(r)

	return r
}
