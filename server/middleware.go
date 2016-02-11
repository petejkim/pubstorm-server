package server

import "github.com/gin-gonic/gin"

func configureMiddleware(r *gin.Engine) {
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
}
