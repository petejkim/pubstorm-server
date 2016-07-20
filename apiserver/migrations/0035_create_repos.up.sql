CREATE TABLE repos (
  id bigserial PRIMARY KEY NOT NULL,

  project_id bigint REFERENCES projects(id) NOT NULL,
  user_id bigint REFERENCES users(id) NOT NULL,

  uri character varying(255) NOT NULL,
  branch character varying(255) DEFAULT 'master',

  webhook_path character varying(255) DEFAULT encode(gen_random_bytes(16), 'hex') NOT NULL,
  webhook_secret character varying(255) DEFAULT '' NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE UNIQUE INDEX index_repos_on_project_id ON repos USING btree (project_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX index_repos_on_webhook_path ON repos USING btree (webhook_path) WHERE deleted_at IS NULL;
