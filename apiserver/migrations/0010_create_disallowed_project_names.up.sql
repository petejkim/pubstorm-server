CREATE TABLE disallowed_project_names (
  id bigserial PRIMARY KEY NOT NULL,

  name character varying(255) NOT NULL
);

CREATE UNIQUE INDEX index_disallowed_project_names_on_name ON disallowed_project_names USING btree (name);
