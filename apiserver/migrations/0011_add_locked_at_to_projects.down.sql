ALTER TABLE projects DROP COLUMN locked_at;
DROP INDEX index_projects_on_locked_at;
