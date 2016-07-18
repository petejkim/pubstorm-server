CREATE TABLE pushes (
  id bigserial PRIMARY KEY NOT NULL,

  repo_id bigint REFERENCES repos(id) NOT NULL,
  deployment_id bigint REFERENCES deployments(id) NOT NULL,

  ref character varying(255) NOT NULL,
  payload text,

  processed_at timestamp without time zone,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_pushes_on_repo_id ON pushes USING btree (repo_id) WHERE deleted_at IS NULL;
CREATE INDEX index_pushes_on_deployment_id ON pushes USING btree (deployment_id) WHERE deleted_at IS NULL;
