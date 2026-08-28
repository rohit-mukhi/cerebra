-- Cerebra dynamic model routing queries

-- Runtime tier and semantic model maps

-- name: GetRuntimeTierModelMap :one
SELECT tier_model_map FROM agent_runtime
WHERE id = $1;

-- name: GetRuntimeSemanticModelMap :one
SELECT semantic_model_map FROM agent_runtime
WHERE id = $1;

-- name: GetRuntimeModelMaps :one
SELECT tier_model_map, semantic_model_map FROM agent_runtime
WHERE id = $1;

-- name: GetAllRuntimeTierModelMaps :many
-- For cross-runtime fallback: scan all runtimes in a workspace to find
-- alternative models when the primary runtime's model is unavailable
SELECT id, tier_model_map, semantic_model_map FROM agent_runtime
WHERE workspace_id = $1;

-- name: UpdateRuntimeTierModelMap :exec
UPDATE agent_runtime
SET tier_model_map = $2
WHERE id = $1;

-- name: UpdateRuntimeSemanticModelMap :exec
UPDATE agent_runtime
SET semantic_model_map = $2
WHERE id = $1;

-- name: UpdateRuntimeModelMaps :exec
UPDATE agent_runtime
SET tier_model_map = $2, semantic_model_map = $3
WHERE id = $1;

-- Session affinity

-- name: GetIssueSessionModel :one
SELECT session_model FROM issue
WHERE id = $1;

-- name: SetIssueSessionModel :exec
UPDATE issue
SET session_model = $2
WHERE id = $1;

-- name: ClearIssueSessionModel :exec
UPDATE issue
SET session_model = NULL
WHERE id = $1;

-- name: GetChatSessionModel :one
SELECT session_model FROM chat_session
WHERE id = $1;

-- name: SetChatSessionModel :exec
UPDATE chat_session
SET session_model = $2
WHERE id = $1;

-- name: ClearChatSessionModel :exec
UPDATE chat_session
SET session_model = NULL
WHERE id = $1;

-- Model unavailability tracking

-- name: UpsertModelUnavailability :exec
INSERT INTO cerebra_model_unavailability (runtime_id, model, marked_at, ttl_seconds)
VALUES ($1, $2, $3, $4)
ON CONFLICT (runtime_id, model)
DO UPDATE SET marked_at = EXCLUDED.marked_at, ttl_seconds = EXCLUDED.ttl_seconds;

-- name: DeleteModelUnavailability :exec
DELETE FROM cerebra_model_unavailability
WHERE runtime_id = $1 AND model = $2;

-- name: GetModelUnavailability :one
SELECT * FROM cerebra_model_unavailability
WHERE runtime_id = $1 AND model = $2;

-- name: GetActiveModelUnavailabilities :many
-- Returns models that are still within their unavailability window
SELECT runtime_id, model, marked_at, ttl_seconds
FROM cerebra_model_unavailability
WHERE marked_at + (ttl_seconds || ' seconds')::interval > now();

-- name: GetRuntimeActiveModelUnavailabilities :many
-- Returns unavailable models for a specific runtime
SELECT model, marked_at, ttl_seconds
FROM cerebra_model_unavailability
WHERE runtime_id = $1
  AND marked_at + (ttl_seconds || ' seconds')::interval > now();

-- name: CleanupExpiredUnavailabilities :exec
-- Cleanup task: remove expired unavailability entries
DELETE FROM cerebra_model_unavailability
WHERE marked_at + (ttl_seconds || ' seconds')::interval <= now();

-- name: IsModelAvailable :one
-- Check if a model is available (not in unavailability cache or expired)
SELECT NOT EXISTS (
  SELECT 1 FROM cerebra_model_unavailability
  WHERE runtime_id = $1
    AND model = $2
    AND marked_at + (ttl_seconds || ' seconds')::interval > now()
) AS is_available;


-- Model Discovery queries

-- name: UpdateRuntimeDiscoveredModels :exec
UPDATE agent_runtime
SET 
    discovered_models = $2,
    tier_model_map = $3,
    updated_at = NOW()
WHERE id = $1;

-- name: GetRuntimeDiscoveredModels :one
SELECT discovered_models 
FROM agent_runtime 
WHERE id = $1;

-- name: CheckRuntimeDiscoveryStatus :one
SELECT 
    discovered_models IS NULL AS needs_discovery,
    updated_at < NOW() - INTERVAL '7 days' AS is_stale
FROM agent_runtime
WHERE id = $1;
