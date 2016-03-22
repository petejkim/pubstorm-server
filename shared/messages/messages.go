package messages

type DeployJobData struct {
	DeploymentID     uint     `json:"deployment_id"`
	DeploymentPrefix string   `json:"deployment_prefix"`
	ProjectName      string   `json:"project_name"`
	Domains          []string `json:"domains"`
}

type V1InvalidationMessageData struct {
	Domains []string `json:"domains"`
}
