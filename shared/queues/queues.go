package queues

const (
	Deploy = "deploy"
)

// make sure to add the queue here too so testhelper can clean it
var All = []string{
	Deploy,
}
