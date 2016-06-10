package esconn

import (
	"os"
	"sync"

	"gopkg.in/olivere/elastic.v2"
)

var (
	client *elastic.Client
	lock   sync.Mutex
)

// ES returns elastic search client
func ES() (*elastic.Client, error) {
	lock.Lock()
	defer lock.Unlock()
	if client == nil {
		var err error
		client, err = elastic.NewClient(elastic.SetURL(os.Getenv("ELASTICSEARCH_URL")), elastic.SetSniff(false))
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}
