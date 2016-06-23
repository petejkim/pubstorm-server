ALTER TABLE projects ADD COLUMN max_deploys_kept bigint DEFAULT 0 NOT NULL;

-- Set a max of 10 for all existing projects.
UPDATE projects SET max_deploys_kept = 10;

-- Lock table to prevent writes.
LOCK TABLE deployments IN EXCLUSIVE MODE;

-- Soft delete all but the latest 10 deployments per project.
CREATE FUNCTION keep_only_last_10_deployments() RETURNS void AS $$
DECLARE
  _project_id bigint;
BEGIN
  FOR _project_id IN SELECT id FROM projects ORDER BY id ASC LOOP
    UPDATE deployments
      SET deleted_at = now()
      WHERE
        project_id = _project_id
        AND state = 'deployed'
        AND deleted_at IS NULL
        AND deployed_at <= (
          SELECT deployed_at FROM deployments
          WHERE
            project_id = _project_id
            AND state = 'deployed'
            AND deleted_at IS NULL
          ORDER BY deployed_at DESC
          LIMIT 1 OFFSET 10
        );
  END LOOP;
END;
$$ LANGUAGE plpgsql;

SELECT keep_only_last_10_deployments();
DROP FUNCTION keep_only_last_10_deployments();
