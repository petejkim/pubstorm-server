CREATE TABLE collabs (
  id bigserial PRIMARY KEY NOT NULL,

  project_id bigint REFERENCES projects(id) NOT NULL,
  user_id bigint REFERENCES users(id) NOT NULL,

  created_at timestamp without time zone DEFAULT now() NOT NULL,
  updated_at timestamp without time zone DEFAULT now() NOT NULL,
  deleted_at timestamp without time zone
);

CREATE INDEX index_collabs_on_user_id ON collabs USING btree (user_id);
CREATE INDEX index_collabs_on_project_id ON collabs USING btree (project_id);
