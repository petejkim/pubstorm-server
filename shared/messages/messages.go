package messages

type DeployJobData struct {
	DeploymentID      uint `json:"deployment_id"`
	SkipWebrootUpload bool `json:"skip_webroot_upload"` // if true, uploading of webroot will be skipped and only meta.json for domains will be deployed
	SkipInvalidation  bool `json:"skip_invalidation"`   // if true, prefix cache invalidation message will not be published
	UseRawBundle      bool `json:"use_raw_bundle"`      // if true, it uses raw bundle to deploy instead of optimized bundle
}

type BuildJobData struct {
	DeploymentID uint `json:"deployment_id"`
}

type PushJobData struct {
	PushID uint `json:"push_id"`
}

type V1InvalidationMessageData struct {
	Domains []string `json:"domains"`
}
