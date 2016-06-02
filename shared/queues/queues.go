package queues

// queue names
const (
	Deploy = "deploy"
	Build  = "build"
)

// make sure to add the queue here too so testhelper can clean it
var All = []string{
	Deploy,
	Build,
}
