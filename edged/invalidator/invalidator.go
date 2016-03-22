package invalidator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	log "github.com/Sirupsen/logrus"
	"github.com/nitrous-io/rise-server/shared/messages"
)

var APIHost = "http://127.0.0.1:8081"

var errRequestFailed = errors.New("Unexpected error on making invalidation request")

func Work(data []byte) error {
	j := &messages.V1InvalidationMessageData{}
	if err := json.Unmarshal(data, j); err != nil {
		return err
	}

	for _, domain := range j.Domains {
		invalidateURL := fmt.Sprintf("%s/invalidate/%s", APIHost, domain)
		res, err := http.PostForm(invalidateURL, url.Values{})
		if err != nil {
			return err
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			output := ""
			if b, err := ioutil.ReadAll(res.Body); err == nil {
				output = string(b)
			}

			log.Errorf("Unexpected error on invalidation request: (%d) %s", res.StatusCode, output)
			return errRequestFailed
		}
	}

	return nil
}
