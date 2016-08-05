ALTER TABLE deployments ADD COLUMN template_id bigint REFERENCES templates(id) DEFAULT NULL;
