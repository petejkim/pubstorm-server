package common

import (
	"os"

	"github.com/nitrous-io/rise-server/pkg/tracker"
)

var Tracker tracker.Trackable = tracker.NewSegmentTracker(os.Getenv("SEGMENT_WRITE_KEY"))

func Identify(userID string, traits, context map[string]interface{}) error {
	return Tracker.Identify(userID, traits, context)
}

func Track(userID, event string, props, context map[string]interface{}) error {
	return Tracker.Track(userID, event, props, context)
}
