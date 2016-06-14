ALTER TABLE projects DROP COLUMN last_digest_sent_at;
DROP INDEX index_projects_on_last_digest_sent_at;
