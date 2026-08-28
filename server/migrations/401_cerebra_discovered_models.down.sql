-- Rollback migration 401: Remove discovered_models column

ALTER TABLE agent_runtime
DROP COLUMN IF EXISTS discovered_models;
