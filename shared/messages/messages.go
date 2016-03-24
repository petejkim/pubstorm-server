package messages

type DeployJobData struct {
	DeploymentID      uint     `json:"deployment_id"`
	DeploymentPrefix  string   `json:"deployment_prefix"`
	ProjectName       string   `json:"project_name"`
	Domains           []string `json:"domains"`
	SkipWebrootUpload bool     `json:"skip_webroot_upload"` // if true, uploading of webroot will be skipped and only meta.json for domains will be deployed
	SkipInvalidation  bool     `json:"skip_invalidation"`   // if true, prefix cache invalidation message will not be published
}

type V1InvalidationMessageData struct {
	Domains []string `json:"domains"`
}
