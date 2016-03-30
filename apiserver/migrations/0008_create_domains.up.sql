CREATE TABLE domains (
  id bigserial PRIMARY KEY NOT NULL,

  project_id bigint REFERENCES projects(id) NOT NULL,
  name character varying(255) NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_domains_on_project_id ON domains USING btree (project_id);
CREATE UNIQUE INDEX index_domains_on_name ON domains USING btree (name) WHERE deleted_at IS NULL;
