package domains

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
)

func Index(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	doms := []*domain.Domain{}
	if err := db.Where("project_id = ?", proj.ID).Find(&doms).Error; err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	domNames := make([]string, 0, len(doms))
	for _, dom := range doms {
		domNames = append(domNames, dom.Name)
	}
	sort.Sort(sort.StringSlice(domNames))

	c.JSON(http.StatusOK, gin.H{
		"domains": append(
			[]string{proj.Name + ".rise.cloud"},
			domNames...,
		),
	})
}

func Create(c *gin.Context) {
	proj := controllers.CurrentProject(c)

	dom := &domain.Domain{
		Name:      c.PostForm("name"),
		ProjectID: proj.ID,
	}

	if errs := dom.Validate(); errs != nil {
		c.JSON(422, gin.H{
			"error":  "invalid_params",
			"errors": errs,
		})
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := db.Create(dom).Error; err != nil {
		if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]interface{}{
					"name": "is taken",
				},
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain": dom.AsJSON(),
	})
}
