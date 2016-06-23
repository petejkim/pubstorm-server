ALTER TABLE projects ADD COLUMN last_digest_sent_at timestamp without time zone;
CREATE INDEX index_projects_on_last_digest_sent_at ON projects USING btree (last_digest_sent_at);

