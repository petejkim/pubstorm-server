package fake

type Tracker struct {
	IdentifyCalls Calls
	TrackCalls    Calls

	IdentifyError error
	TrackError    error
}

func (t *Tracker) Identify(userID string, traits, context map[string]interface{}) error {
	t.IdentifyCalls.Add(List{userID, traits, context}, List{t.IdentifyError}, nil)

	return t.IdentifyError
}

func (t *Tracker) Track(userID, event string, props, context map[string]interface{}) error {
	t.TrackCalls.Add(List{userID, event, props, context}, List{t.TrackError}, nil)

	return t.TrackError
}
