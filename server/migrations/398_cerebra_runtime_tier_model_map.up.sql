-- Add tier_model_map and semantic_model_map columns to agent_runtime table
-- These support Cerebra dynamic model routing

ALTER TABLE agent_runtime
  ADD COLUMN tier_model_map JSONB,
  ADD COLUMN semantic_model_map JSONB;

COMMENT ON COLUMN agent_runtime.tier_model_map IS 'Maps complexity tiers (simple, standard, heavy) to model IDs for dynamic routing';
COMMENT ON COLUMN agent_runtime.semantic_model_map IS 'Maps semantic domains (code, math, creative, search, data) to specialist model IDs';
