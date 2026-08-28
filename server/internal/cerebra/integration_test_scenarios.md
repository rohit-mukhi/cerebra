# Cerebra Integration Test Scenarios

This document outlines integration test scenarios for the Cerebra dynamic model router. These tests require a running database and server.

## Test Setup Requirements

- Running PostgreSQL database with migrations applied (398-400)
- Server process running
- At least one configured runtime with model maps
- Test workspace, agents, and tasks

## Routing Logic Tests

### Test 1: Semantic Routing - Code Domain

**Setup:**
```sql
UPDATE agent_runtime 
SET semantic_model_map = '{"code": "qwen-coder"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue: "Fix bug in authentication function and add unit tests"
2. Assign to agent bound to test runtime
3. Wait for task to be claimed and executed

**Expected:**
- Daemon logs show: `cerebra: semantic routing selected model` with `domain=code`
- Task executes with model=`qwen-coder`
- Keywords matched: `bug`, `function`, `test`

**Validation:**
```bash
grep "cerebra: semantic routing" daemon.log | grep "qwen-coder"
```

### Test 2: Semantic Routing - Math Domain

**Setup:**
```sql
UPDATE agent_runtime 
SET semantic_model_map = '{"math": "deepseek-r1"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue: "Calculate integral of polynomial equation"
2. Assign to agent

**Expected:**
- Semantic routing to `deepseek-r1`
- Domain: `math`
- Keywords: `calculate`, `integral`, `equation`

### Test 3: Tier Routing - Simple Tier

**Setup:**
```sql
UPDATE agent_runtime 
SET tier_model_map = '{"simple": "gpt-4o-mini", "standard": "gpt-4o", "heavy": "claude-opus-4-5"}'::jsonb,
    semantic_model_map = NULL
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue: "Fix typo in README"
2. Assign to agent

**Expected:**
- No semantic match (no domain keywords)
- Tier routing to `gpt-4o-mini`
- Tier: `simple`
- Reason: Short prompt, no complex keywords

### Test 4: Tier Routing - Heavy Tier

**Setup:**
```sql
-- Same as Test 3
```

**Test Steps:**
1. Create issue: "Refactor entire authentication architecture to support multi-tenant OAuth2 with PKCE"
2. Assign to agent

**Expected:**
- Tier routing to `claude-opus-4-5`
- Tier: `heavy`
- Reason: Long prompt + heavy keywords (`refactor`, `architecture`)

### Test 5: Semantic Takes Priority Over Tier

**Setup:**
```sql
UPDATE agent_runtime 
SET tier_model_map = '{"simple": "gpt-4o-mini", "heavy": "claude-opus-4-5"}'::jsonb,
    semantic_model_map = '{"code": "qwen-coder"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue: "Refactor the entire authentication module and add comprehensive test coverage"
2. Assign to agent

**Expected:**
- Semantic routing wins (code domain matched)
- Model: `qwen-coder`
- NOT `claude-opus-4-5` even though prompt has "refactor" (heavy keyword)
- Domain takes priority over tier

### Test 6: Fallback to Static Model on Error

**Setup:**
```sql
-- Temporarily make API endpoint fail (e.g., wrong URL in client or network issue)
```

**Test Steps:**
1. Create issue with any prompt
2. Assign to agent

**Expected:**
- Daemon logs: `cerebra: failed to fetch model maps`
- Task proceeds with static model from agent configuration
- Task does NOT fail
- Graceful degradation

### Test 7: No Routing Configuration

**Setup:**
```sql
UPDATE agent_runtime 
SET tier_model_map = NULL,
    semantic_model_map = NULL
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue with any prompt
2. Assign to agent

**Expected:**
- No routing attempted (maps are empty)
- Uses static model from agent configuration
- No cerebra log entries

### Test 8: Empty Prompt

**Setup:**
```sql
-- Any valid routing configuration
```

**Test Steps:**
1. Create task with empty/blank prompt (if possible)
2. Assign to agent

**Expected:**
- buildRoutingPrompt() returns empty string
- No routing attempted
- Uses static model

## Unavailability Tracking Tests

### Test 9: Quota Error Detection

**Setup:**
```sql
-- Simulate quota exhaustion by using expired API key or hitting actual limit
```

**Test Steps:**
1. Configure agent with model that will hit quota
2. Create issue and let task execute
3. Wait for quota error in task output

**Expected:**
- Daemon logs: `detected quota/rate limit error in task output`
- Daemon calls MarkModelUnavailable API
- Database entry in `cerebra_model_unavailability` table with TTL=3600
- Subsequent tasks skip this model

**Validation:**
```sql
SELECT * FROM cerebra_model_unavailability 
WHERE model = '<exhausted-model>';
```

### Test 10: Context Error Does NOT Mark Unavailable

**Setup:**
```sql
-- Use very long prompt that exceeds model context window
```

**Test Steps:**
1. Create issue with extremely long description (>200k tokens)
2. Let task fail with context_length_exceeded error

**Expected:**
- Task fails with context error
- Model is NOT marked unavailable
- No entry in `cerebra_model_unavailability`
- Next task with shorter prompt can still use the model

### Test 11: Unavailability Expiration

**Setup:**
```sql
INSERT INTO cerebra_model_unavailability (runtime_id, model, marked_at, ttl_seconds)
VALUES ('<runtime-id>', 'test-model', NOW() - INTERVAL '2 hours', 3600);
```

**Test Steps:**
1. Query IsModelAvailable for expired model

**Expected:**
- Returns `is_available = true` (TTL expired)
- Cleanup job removes expired entry

## Session Affinity Tests

### Test 12: First Turn Sets Session Model

**Setup:**
```sql
UPDATE agent_runtime 
SET tier_model_map = '{"simple": "gpt-4o-mini", "heavy": "claude-opus-4-5"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Create issue: "Fix typo" (simple tier)
2. Check issue.session_model after task completes

**Expected:**
```sql
SELECT session_model FROM issue WHERE id = '<issue-id>';
-- Returns: gpt-4o-mini
```

### Test 13: Follow-Up Turn Reuses Session Model

**Setup:**
```sql
-- Issue from Test 12 with session_model = 'gpt-4o-mini'
```

**Test Steps:**
1. Add comment to same issue: "Also update copyright year" (still simple)
2. Check model used for second task

**Expected:**
- Second task uses `gpt-4o-mini` (session affinity)
- NOT re-routed even though new prompt could score differently
- session_model unchanged

### Test 14: Session Escalation

**Setup:**
```sql
-- Issue from Test 12 with session_model = 'gpt-4o-mini'
```

**Test Steps:**
1. Add comment: "Refactor the entire authentication architecture" (heavy tier)
2. Check model used and session_model

**Expected:**
- Escalation to `claude-opus-4-5`
- session_model updated to `claude-opus-4-5`
- Subsequent turns use heavy model

### Test 15: No De-Escalation

**Setup:**
```sql
-- Issue with session_model = 'claude-opus-4-5' (heavy)
```

**Test Steps:**
1. Add comment: "Fix typo" (simple tier)
2. Check model used

**Expected:**
- Still uses `claude-opus-4-5` (no de-escalation)
- session_model unchanged
- Simple task runs on heavy model

### Test 16: Chat Session Affinity

**Setup:**
```sql
UPDATE agent_runtime 
SET semantic_model_map = '{"code": "qwen-coder"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Start chat: "How do I fix this bug?"
2. Check chat_session.session_model
3. Continue chat: "Show me the code"
4. Verify same model used

**Expected:**
```sql
SELECT session_model FROM chat_session WHERE id = '<session-id>';
-- Returns: qwen-coder

-- Second turn uses same model
```

## Error Handling Tests

### Test 17: Session Affinity API Failure

**Setup:**
```sql
-- Temporarily disable session model endpoints
```

**Test Steps:**
1. Create issue, let routing happen
2. Check daemon logs

**Expected:**
- Routing still happens successfully
- Warning logged: `failed to update issue session model; session affinity may not work`
- Task proceeds normally
- No entry in issue.session_model

### Test 18: Invalid Model Configuration

**Setup:**
```sql
UPDATE agent_runtime 
SET tier_model_map = '{"invalid_tier": "some-model"}'::jsonb
WHERE id = '<test-runtime-id>';
```

**Test Steps:**
1. Try to update via API

**Expected:**
- API returns 400 Bad Request
- Error: "invalid tier: invalid_tier"
- tier_model_map unchanged

### Test 19: Empty Model in Configuration

**Setup:**
```sql
-- Try via API
PATCH /api/runtimes/<id>
{"tier_model_map": {"simple": ""}}
```

**Expected:**
- API returns 400 Bad Request
- Validation fails on empty string

## Performance Tests

### Test 20: Routing Latency

**Setup:**
- Configure runtime with both maps

**Test Steps:**
1. Create 100 issues with various prompts
2. Measure time from claim to task start

**Expected:**
- Routing adds <50ms overhead per task
- No noticeable delay in task execution
- API calls don't block claim response

### Test 21: Concurrent Routing

**Setup:**
- Multiple runtimes with different configurations

**Test Steps:**
1. Create 50 concurrent tasks across different runtimes
2. Monitor database locks and API response times

**Expected:**
- No deadlocks
- All tasks route successfully
- API endpoints handle concurrent requests

## Test Execution Commands

```bash
# Run all cerebra unit tests
cd server && go test ./internal/cerebra/... -v

# Run handler tests (requires testdb)
cd server && go test ./internal/handler/... -run TestSetIssueSessionModel -v

# Check routing in daemon logs
tail -f logs/daemon.log | grep cerebra

# Query routing configuration
psql $DATABASE_URL -c "SELECT id, tier_model_map, semantic_model_map FROM agent_runtime;"

# Check session models
psql $DATABASE_URL -c "SELECT id, session_model FROM issue WHERE session_model IS NOT NULL;"

# Check unavailability entries
psql $DATABASE_URL -c "SELECT * FROM cerebra_model_unavailability;"
```

## Cleanup After Tests

```sql
-- Reset runtime configurations
UPDATE agent_runtime 
SET tier_model_map = NULL,
    semantic_model_map = NULL;

-- Clear session models
UPDATE issue SET session_model = NULL;
UPDATE chat_session SET session_model = NULL;

-- Clear unavailability entries
DELETE FROM cerebra_model_unavailability;
```

## Notes

- These tests require manual execution with a running system
- Automated integration tests would need fixtures and test database setup
- See existing `daemon_test.go` and `daemon_claim_*_test.go` for patterns
- Session affinity escalation logic is not yet implemented (Phase 4+)
