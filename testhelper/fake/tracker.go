package fake

type Tracker struct {
	IdentifyCalls Calls
	TrackCalls    Calls

	IdentifyError error
	TrackError    error
}

func (t *Tracker) Identify(userID, anonymousID string, traits, context map[string]interface{}) error {
	t.IdentifyCalls.Add(List{userID, anonymousID, traits, context}, List{t.IdentifyError}, nil)

	return t.IdentifyError
}

func (t *Tracker) Track(userID, event, anonymousID string, props, context map[string]interface{}) error {
	t.TrackCalls.Add(List{userID, event, anonymousID, props, context}, List{t.TrackError}, nil)

	return t.TrackError
}
