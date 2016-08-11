package tracker

type Trackable interface {
	Identify(userID, anonymousID string, traits, context map[string]interface{}) error
	Track(userID, event, anonymousID string, props, context map[string]interface{}) error
	Alias(userID, previousID string) error
}
