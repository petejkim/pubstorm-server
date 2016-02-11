package ping_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/petejkim/rise-server/server"
)

var s *httptest.Server

func setUp() {
	s = httptest.NewServer(server.New())
}

func tearDown() {
	s.Close()
}

func TestPing(t *testing.T) {
	setUp()
	defer tearDown()

	res, err := http.Get(s.URL + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var j map[string]string
	if err := json.NewDecoder(res.Body).Decode(&j); err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != 200 {
		t.Fatal("Expected status code to be 200 OK")
	}

	if j["message"] != "pong" {
		t.Fatal("Expected JSON message to contain pong, got %s", j["message"])
	}
}
