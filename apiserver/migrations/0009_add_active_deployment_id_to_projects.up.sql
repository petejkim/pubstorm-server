ALTER TABLE projects ADD COLUMN active_deployment_id bigint REFERENCES deployments(id);
