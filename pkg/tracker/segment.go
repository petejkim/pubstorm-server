package tracker

import "github.com/segmentio/analytics-go"

// SegmentTracker wraps around analytics.Client to conform to the Trackable
// interface. This makes it a better Go citizen and more amenable to testing.
type SegmentTracker struct {
	*analytics.Client
}

func NewSegmentTracker(writeKey string) Trackable {
	client := analytics.New(writeKey)
	return &SegmentTracker{Client: client}
}

func (t *SegmentTracker) Identify(userID string, traits, context map[string]interface{}) error {
	return t.Client.Identify(&analytics.Identify{
		UserId:  userID,
		Context: context,
		Traits:  traits,
	})
}

func (t *SegmentTracker) Track(userID, event string, props, context map[string]interface{}) error {
	return t.Client.Track(&analytics.Track{
		UserId:     userID,
		Event:      event,
		Properties: props,
		Context:    context,
	})
}
