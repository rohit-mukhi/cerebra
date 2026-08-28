-- Migration 401: Add discovered_models column to agent_runtime for caching available models
-- This avoids repeated model list queries and enables automatic tier_model_map population

ALTER TABLE agent_runtime
ADD COLUMN discovered_models JSONB DEFAULT NULL;

COMMENT ON COLUMN agent_runtime.discovered_models IS 
'Cached list of models available from this runtime, discovered via the model list API. Format: [{"id": "gpt-4o", "name": "GPT-4o", "tier": "standard"}, ...]. NULL means discovery has not run yet.';
