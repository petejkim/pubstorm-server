package hooks

import "strings"

type payload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Forced     bool   `json:"forced"`
	CompareURL string `json:"compare"`
	Repository struct {
		FullName   string `json:"full_name"`
		ArchiveURL string `json:"archive_url"`
	} `json:"repository"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
}

func (p *payload) branch() string {
	return strings.TrimPrefix(p.Ref, "refs/heads/")
}
