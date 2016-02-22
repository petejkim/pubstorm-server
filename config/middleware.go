package config

import "github.com/gin-gonic/gin"

func Middleware(r *gin.Engine) {
	if gin.Mode() != gin.TestMode {
		r.Use(gin.Logger())
		r.Use(gin.Recovery())
	}
}
