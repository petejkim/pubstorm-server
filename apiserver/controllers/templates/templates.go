package templates

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/template"
)

func Index(c *gin.Context) {
	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var tmpls []*template.Template
	if err := db.Order("rank ASC").Find(&tmpls).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	tmplsAsJson := []interface{}{}
	for _, tmpl := range tmpls {
		tmplsAsJson = append(tmplsAsJson, tmpl.AsJSON())
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": tmplsAsJson,
	})
}
