CREATE TABLE deployments (
  id bigserial PRIMARY KEY NOT NULL,

  project_id bigint REFERENCES projects(id) NOT NULL,
  user_id bigint REFERENCES users(id) NOT NULL,

  state character varying(255) DEFAULT 'pending' NOT NULL,
  prefix character varying(255) DEFAULT encode(gen_random_bytes(2), 'hex') NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_deployments_on_user_id ON deployments USING btree (user_id);
CREATE INDEX index_deployments_on_project_id ON deployments USING btree (project_id);
