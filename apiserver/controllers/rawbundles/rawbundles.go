package rawbundles

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/rawbundle"
)

func Get(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	bun := &rawbundle.RawBundle{}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := db.Where("project_id = ? AND checksum = ?", proj.ID, c.Param("bundle_checksum")).First(bun).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "raw bundle could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"raw_bundle": bun.AsJSON(),
	})
}
