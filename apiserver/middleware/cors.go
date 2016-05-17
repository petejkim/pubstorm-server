package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS(c *gin.Context) {
	req := c.Request

	// handle preflight request
	if req.Method == http.MethodOptions {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,PATCH,DELETE,OPTIONS")
		if h := req.Header["Access-Control-Request-Headers"]; h != nil {
			c.Header("Access-Control-Allow-Headers", strings.Join(h, ","))
		}
		c.Header("Access-Control-Allow-Credentials", "true")
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
		c.Abort()
		return
	}

	c.Header("Access-Control-Allow-Origin", "*")
	c.Next()
}
