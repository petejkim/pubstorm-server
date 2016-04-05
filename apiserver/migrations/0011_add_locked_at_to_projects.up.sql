ALTER TABLE projects ADD COLUMN locked_at timestamp without time zone;
CREATE INDEX index_projects_on_locked_at ON projects USING btree (locked_at);
