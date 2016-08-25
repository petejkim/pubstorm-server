package common

import (
	"net"
	"net/http"
	"os"

	"github.com/nitrous-io/rise-server/pkg/tracker"
)

var Tracker tracker.Trackable = tracker.NewSegmentTracker(os.Getenv("SEGMENT_WRITE_KEY"))

func Identify(userID, anonymousID string, traits, context map[string]interface{}) error {
	return Tracker.Identify(userID, anonymousID, traits, context)
}

func Track(userID, event, anonymousID string, props, context map[string]interface{}) error {
	return Tracker.Track(userID, event, anonymousID, props, context)
}

func Alias(userID, previousID string) error {
	return Tracker.Alias(userID, previousID)
}

func GetIP(r *http.Request) string {
	if ipProxy := r.Header.Get("X-FORWARDED-FOR"); len(ipProxy) > 0 {
		return ipProxy
	}
	if ipProxy := r.Header.Get("x-forwarded-for"); len(ipProxy) > 0 {
		return ipProxy
	}
	if ipProxy := r.Header.Get("X-Forwarded-For"); len(ipProxy) > 0 {
		return ipProxy
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
