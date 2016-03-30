CREATE TABLE projects (
  id bigserial PRIMARY KEY NOT NULL,

  name character varying(255) NOT NULL,
  user_id bigint REFERENCES users(id) NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_projects_on_user_id ON projects USING btree (user_id);
CREATE UNIQUE INDEX index_projects_on_name ON projects USING btree (name) WHERE deleted_at IS NULL;
