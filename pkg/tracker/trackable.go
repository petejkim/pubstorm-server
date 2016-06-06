package tracker

type Trackable interface {
	Identify(userID string, traits, context map[string]interface{}) error
	Track(userID, event string, props, context map[string]interface{}) error
}
