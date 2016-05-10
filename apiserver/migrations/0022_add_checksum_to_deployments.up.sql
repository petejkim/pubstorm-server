ALTER TABLE deployments ADD COLUMN checksum character varying(255) DEFAULT '';
CREATE INDEX index_deployments_on_checksum ON deployments USING btree (checksum) WHERE deleted_at IS NULL AND length(checksum) > 0;
