-- lock table to prevent writes
LOCK TABLE deployments IN EXCLUSIVE MODE;

ALTER TABLE deployments ADD COLUMN version bigint DEFAULT 0 NOT NULL;

-- insert version to all existing deployments
CREATE FUNCTION version_all_deployments() RETURNS void AS $$
DECLARE
  _project_id bigint;
  _deployment_id bigint;
  _version bigint;
BEGIN
  FOR _project_id IN SELECT id FROM projects ORDER BY id ASC LOOP
    FOR _deployment_id IN SELECT id FROM deployments WHERE project_id = _project_id ORDER BY id ASC LOOP
      UPDATE projects SET version_counter = version_counter + 1 WHERE id = _project_id RETURNING version_counter INTO _version;
      UPDATE deployments SET version = _version WHERE id = _deployment_id;
    END LOOP;
  END LOOP;
END;
$$ LANGUAGE plpgsql;

SELECT version_all_deployments();
DROP FUNCTION version_all_deployments();

ALTER TABLE deployments ADD CONSTRAINT constraint_deployments_project_id_version UNIQUE (project_id, version);
