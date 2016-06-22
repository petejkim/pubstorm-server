package common

import (
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
