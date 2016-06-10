package stats

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/stat"
)

func Index(c *gin.Context) {
	if c.Query("token") != common.StatsToken {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_admin_token",
			"error_description": "admin token is required",
		})
		return
	}

	filters := map[string]int64{}
	requiredParams := []string{"project_id", "year", "month"}
	for _, requiredParam := range requiredParams {
		queryParam := c.Query(requiredParam)
		if queryParam == "" {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					requiredParam: "is required",
				},
			})
			return
		}

		value, err := strconv.ParseInt(queryParam, 10, 64)
		if err != nil {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					requiredParam: "is invalid",
				},
			})
			return
		}

		filters[requiredParam] = value
	}

	year := int(filters["year"])
	month := time.Month(filters["month"])

	if year < 0 {
		c.JSON(422, gin.H{
			"error": "invalid_params",
			"errors": map[string]string{
				"year": "is invalid",
			},
		})
		return
	}

	if month < time.January || month > time.December {
		c.JSON(422, gin.H{
			"error": "invalid_params",
			"errors": map[string]string{
				"month": "is invalid",
			},
		})
		return
	}

	from := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC).Add(-1 * time.Second)

	stats, err := stat.GetProjectStat(filters["project_id"], from, to)
	if err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(422, gin.H{
				"error": "invalid_params",
				"errors": map[string]string{
					"project_id": "project could not be found",
				},
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}
