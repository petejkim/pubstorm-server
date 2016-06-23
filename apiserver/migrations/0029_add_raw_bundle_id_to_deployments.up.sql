ALTER TABLE deployments ADD COLUMN raw_bundle_id bigint;
ALTER TABLE deployments ADD CONSTRAINT deployments_raw_bundle_id_fkey FOREIGN KEY (raw_bundle_id) REFERENCES raw_bundles (id);
CREATE INDEX index_deployments_on_raw_bundle_id ON deployments USING btree (raw_bundle_id) WHERE deleted_at IS NULL AND raw_bundle_id IS NOT NULL;

UPDATE deployments d SET raw_bundle_id = rb.id
  FROM raw_bundles rb
  WHERE rb.uploaded_path = 'deployments/' || d.prefix || '-' || d.id || '/raw-bundle.tar.gz'
    AND d.state IN ('uploaded', 'pending_deploy', 'deploy') ANd d.deleted_at IS NULL;
