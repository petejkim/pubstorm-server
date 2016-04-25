ALTER TABLE deployments DROP CONSTRAINT constraint_deployments_project_id_version;
ALTER TABLE deployments DROP COLUMN version;
