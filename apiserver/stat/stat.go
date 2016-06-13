package stat

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/olivere/elastic.v2"

	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/project"
	"github.com/nitrous-io/rise-server/pkg/esconn"
)

const NumTopPage = 5

type DomainStat struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`

	DomainName string `json:"domain_name"`

	TotalBandwidth  float64    `json:"total_bandwidth"`
	TotalRequests   float64    `json:"total_requests"`
	UniqueVisitors  float64    `json:"unique_visitors"`
	TotalPageViews  float64    `json:"total_page_views"`
	AvgResponseTime float64    `json:"avg_response_time"`
	TopPageViews    []PageView `json:"top_page_views"`
}

type PageView struct {
	Path  string `json:"key"`
	Count int64  `json:"doc_count"`
}

type aggregation struct {
	Value float64 `json:"value"`
}

var GetDomainStat = getDomainStat

func GetProjectStat(projectID int64, from time.Time, to time.Time) ([]*DomainStat, error) {
	index := fmt.Sprintf("logstash-*")

	if from.Year() == to.Year() {
		if from.Month() == to.Month() {
			index = fmt.Sprintf("logstash-%04d.%02d*", from.Year(), from.Month())
		} else {
			index = fmt.Sprintf("logstash-%04d*", from.Year())
		}
	}

	db, err := dbconn.DB()
	if err != nil {
		return nil, err
	}

	proj := &project.Project{}
	if err := db.Where("id = ? AND active_deployment_id IS NOT NULL", projectID).First(proj).Error; err != nil {
		return nil, err
	}

	domainNames, err := proj.DomainNames(db)
	if err != nil {
		return nil, err
	}

	var domainStats []*DomainStat
	for _, domainName := range domainNames {
		stat, err := GetDomainStat(index, domainName, from, to)
		if err != nil {
			return nil, err
		}
		domainStats = append(domainStats, stat)
	}

	return domainStats, nil
}

func getDomainStat(index string, domain string, from time.Time, to time.Time) (*DomainStat, error) {
	client, err := esconn.ES()
	if err != nil {
		return nil, err
	}

	domainStat := &DomainStat{
		DomainName: domain,
		From:       from,
		To:         to,
	}

	rangeFilter := elastic.NewRangeQuery("request_timestamp").From(from).To(to)
	query := elastic.NewBoolQuery().Must(rangeFilter, elastic.NewTermQuery("domain", domain))
	result, err := client.Search().
		Index(index).
		Query(query).
		Aggregation("total_requests", elastic.NewValueCountAggregation().Field("request.raw")).
		Aggregation("total_bandwidth", elastic.NewSumAggregation().Field("bytes")).
		Aggregation("avg_response_time", elastic.NewAvgAggregation().Field("duration")).
		Aggregation("unique_visitors", elastic.NewCardinalityAggregation().Field("ip.raw")).
		Size(0).
		Do()
	if err != nil {
		return nil, err
	}

	if len(result.Aggregations) == 0 {
		return nil, nil
	}

	var totalRequests, totalBandwidth, avgResponseTime, uniqueVisitors aggregation
	if err := json.Unmarshal(*result.Aggregations["total_requests"], &totalRequests); err != nil {
		return nil, err
	}
	domainStat.TotalRequests = totalRequests.Value

	if err := json.Unmarshal(*result.Aggregations["total_bandwidth"], &totalBandwidth); err != nil {
		return nil, err
	}
	domainStat.TotalBandwidth = totalBandwidth.Value

	if err := json.Unmarshal(*result.Aggregations["avg_response_time"], &avgResponseTime); err != nil {
		return nil, err
	}
	domainStat.AvgResponseTime = avgResponseTime.Value

	if err := json.Unmarshal(*result.Aggregations["unique_visitors"], &uniqueVisitors); err != nil {
		return nil, err
	}
	domainStat.UniqueVisitors = uniqueVisitors.Value

	query = elastic.NewBoolQuery().Must(rangeFilter, elastic.NewRegexpQuery("request.raw", ".*/([^/]+html?)?"), elastic.NewTermQuery("domain", domain))
	result, err = client.Search().
		Index(index).
		Query(query).
		Size(0).
		Aggregation("top_pages", elastic.NewTermsAggregation().Field("request.raw").Size(NumTopPage)).
		Aggregation("total_page_views", elastic.NewValueCountAggregation().Field("request.raw")).
		Do()
	if err != nil {
		return nil, err
	}

	if len(result.Aggregations) == 0 {
		return nil, nil
	}

	var totalPageViews aggregation
	if err := json.Unmarshal(*result.Aggregations["total_page_views"], &totalPageViews); err != nil {
		return nil, err
	}
	domainStat.TotalPageViews = totalPageViews.Value

	var termAgg struct {
		Buckets []PageView `json:"buckets"`
	}

	if err := json.Unmarshal(*result.Aggregations["top_pages"], &termAgg); err != nil {
		return nil, err
	}
	domainStat.TopPageViews = termAgg.Buckets

	return domainStat, nil
}
