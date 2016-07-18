package stats_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/deployment"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/apiserver/server"
	"github.com/nitrous-io/rise-server/apiserver/stat"
	"github.com/nitrous-io/rise-server/testhelper"
	"github.com/nitrous-io/rise-server/testhelper/factories"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "stats")
}

var _ = Describe("Stats", func() {
	var (
		db  *gorm.DB
		s   *httptest.Server
		res *http.Response
		err error
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("Index", func() {
		var (
			orgStatsToken string
			params        url.Values

			proj *project.Project
		)

		BeforeEach(func() {
			orgStatsToken = common.StatsToken
			common.StatsToken = "statssecret"

			proj = factories.Project(db, nil)
			depl := factories.Deployment(db, proj, nil, deployment.StateDeployed)

			proj.ActiveDeploymentID = &depl.ID
			Expect(db.Save(proj).Error).To(BeNil())

			params = url.Values{
				"project_id": {strconv.Itoa(int(proj.ID))},
				"year":       {"2016"},
				"month":      {"6"},
				"day":        {"1"},
				"token":      {common.StatsToken},
			}
		})

		AfterEach(func() {
			common.StatsToken = orgStatsToken
		})

		doRequest := func() {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("GET", s.URL+"/admin/stats", params, nil, nil)
			Expect(err).To(BeNil())
		}

		DescribeTable("missing or invalid params",
			func(setUp func(), expectedBody string) {
				setUp()

				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(422))
				Expect(b.String()).To(MatchJSON(expectedBody))
			},

			Entry("missing project_id", func() {
				params.Del("project_id")
			}, `{
				"error": "invalid_params",
				"errors": {
					"project_id": "is required"
				}
			}`),
			Entry("no project with active deployment", func() {
				params.Set("project_id", strconv.Itoa(int(proj.ID+uint(1))))
			}, `{
				"error": "invalid_params",
				"errors": {
					"project_id": "project could not be found"
				}
			}`),
			Entry("missing year", func() {
				params.Del("year")
			}, `{
				"error": "invalid_params",
				"errors": {
					"year": "is required"
				}
			}`),
			Entry("missing month", func() {
				params.Del("month")
			}, `{
				"error": "invalid_params",
				"errors": {
					"month": "is required"
				}
			}`),
			Entry("missing day", func() {
				params.Del("day")
			}, `{
				"error": "invalid_params",
				"errors": {
					"day": "is required"
				}
			}`),
			Entry("invalid project_id", func() {
				params.Set("project_id", "invalidID")
			}, `{
				"error": "invalid_params",
				"errors": {
					"project_id": "is invalid"
				}
			}`),
			Entry("invalid year", func() {
				params.Set("year", "-1")
			}, `{
				"error": "invalid_params",
				"errors": {
					"year": "is invalid"
				}
			}`),
			Entry("invalid month", func() {
				params.Set("month", "-10")
			}, `{
				"error": "invalid_params",
				"errors": {
					"month": "is invalid"
				}
			}`),
			Entry("invalid day", func() {
				params.Set("day", "-1")
			}, `{
				"error": "invalid_params",
				"errors": {
					"day": "is invalid"
				}
			}`),
		)

		Context("when all params are valid", func() {
			It("returns stats", func() {
				stat.GetDomainStat = func(index string, domain string, from time.Time, to time.Time) (*stat.DomainStat, error) {
					return &stat.DomainStat{
						From:            time.Date(2016, 6, 1, 0, 0, 0, 0, time.UTC).Add(-7 * 24 * time.Hour),
						To:              time.Date(2016, 6, 1, 0, 0, 0, 0, time.UTC),
						DomainName:      domain,
						TotalBandwidth:  60000.0,
						TotalRequests:   100.0,
						UniqueVisitors:  200.0,
						TotalPageViews:  300.0,
						AvgResponseTime: 10.0,
						TopPageViews: []stat.PageView{
							stat.PageView{Path: "/", Count: 100},
							stat.PageView{Path: "/pricing", Count: 10},
						},
					}, nil
				}

				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(fmt.Sprintf(`{
					"stats": [{
								"from":"2016-05-25T00:00:00Z",
								"to":"2016-06-01T00:00:00Z",
								"domain_name":"%s",
								"total_bandwidth": 60000,
								"total_requests": 100,
								"unique_visitors":200,
								"total_page_views":300,
								"avg_response_time":10,
								"top_page_views": [
										{"key":"/","doc_count":100},
										{"key":"/pricing","doc_count":10}
								]
					}]}`, proj.DefaultDomainName())))
			})
		})

		Context("token is invalid", func() {
			BeforeEach(func() {
				params.Set("token", "invalidone")
			})

			It("returns 401 unauthorized", func() {
				doRequest()

				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_admin_token",
					"error_description": "admin token is required"
				}`))
			})
		})
	})
})
