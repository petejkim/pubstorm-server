CREATE TABLE raw_bundles (
  id bigserial PRIMARY KEY NOT NULL,

  project_id bigint REFERENCES projects(id) NOT NULL,

  checksum character varying(255),
  uploaded_path character varying(255) NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_raw_bundles_on_project_id ON raw_bundles USING btree (project_id) WHERE deleted_at IS NULL;
CREATE INDEX index_raw_bundles_on_checksum ON raw_bundles USING btree (checksum) WHERE deleted_at IS NULL;

INSERT INTO raw_bundles (project_id, uploaded_path, created_at, updated_at)
  SELECT project_id, 'deployments/' || prefix || '-' || id || '/raw-bundle.tar.gz', now(), now()
  FROM deployments
  WHERE state IN ('uploaded', 'pending_deploy', 'deployed')
    AND deleted_at IS NULL;
