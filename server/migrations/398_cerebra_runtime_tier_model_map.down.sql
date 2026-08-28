-- Rollback: Remove Cerebra routing columns from agent_runtime

ALTER TABLE agent_runtime
  DROP COLUMN IF EXISTS tier_model_map,
  DROP COLUMN IF EXISTS semantic_model_map;
