# Cerebra — Dynamic Model Router

## What It Does

Cerebra is a dynamic model router built into the Multica daemon. It intercepts every task before the agent CLI spawns and replaces the static model selection with one chosen at runtime based on the complexity of the task prompt.

Instead of every task always using the same model configured on the agent, Cerebra scores the prompt, picks the cheapest model that can handle it, and falls back to a stronger model if the first choice is unavailable.

---

## The Core Insight

Every agent task in Multica ultimately becomes a CLI command like:

```
claude --model claude-haiku-3-5 -p "<prompt>"
codex --model gpt-4o-mini "<prompt>"
```

The model is injected as a single field — `agent.ExecOptions.Model` — right before the CLI spawns. Cerebra intercepts at exactly that point in `server/internal/daemon/daemon.go`:

```go
// Existing code resolves a static model:
model := ""
if task.Agent != nil && task.Agent.Model != "" {
    model = task.Agent.Model
}

// Cerebra overrides it dynamically:
if routedModel := d.cerebra.Route(task.Prompt, model); routedModel != "" {
    model = routedModel
}
```

That single override is the entire integration surface. Everything else is the logic that feeds into `Route()`.

---

## How Model Selection Works Today (Without Cerebra)

The resolution order before Cerebra:

1. `task.Agent.Model` — model pinned on the agent record in the DB
2. `entry.Model` — fallback from the runtime's detected default (env var tier)
3. `resolveTaskModelSelection()` — qualifies/validates the model against the local CLI catalog
4. `agent.ExecOptions{ Model: model }` — passed to the agent backend
5. Backend translates to `--model <value>` in the spawned CLI command

The problem: step 1 is static. Every task for that agent uses the same model regardless of whether the task is "fix a typo" or "refactor the entire auth system".

---

## Routing Pipeline

Cerebra runs two routing passes in order. Semantic routing runs first and wins if it finds a domain match. Tier routing is the fallback.

```
prompt
  ↓
1. Semantic match → specialist model found? → use it
  ↓ no match
2. Complexity score → tier → tier_model_map → use tier model
  ↓ no map / no match
3. Static model (daemon default, no routing)
```

---

## Semantic Routing

`cerebra/semantic.go` runs before the complexity scorer. It scans the prompt for domain keywords and maps them to specialist models configured on the runtime.

### How It Works

The runtime has a `semantic_model_map` JSONB column (separate from `tier_model_map`) that maps domain names to model IDs:

```json
{
  "code":     "qwen-coder",
  "math":     "deepseek-r1",
  "creative": "claude-opus-4-5",
  "search":   "grok-3"
}
```

Cerebra uses **hardcoded keyword sets per domain** to match against the prompt. The user only needs to say which model handles which domain — the keyword matching is built in.

### Hardcoded Keyword Sets

| Domain key | Keywords matched in prompt |
|---|---|
| `code` | `code`, `function`, `class`, `bug`, `implement`, `api`, `script`, `compile`, `syntax`, `refactor`, `library`, `module`, `import`, `variable`, `algorithm`, `program`, `debug`, `error`, `exception`, `test` |
| `math` | `math`, `calculate`, `equation`, `formula`, `integral`, `derivative`, `proof`, `theorem`, `matrix`, `vector`, `probability`, `statistics`, `algebra`, `geometry`, `compute`, `solve`, `numerical` |
| `creative` | `write`, `story`, `poem`, `creative`, `essay`, `narrative`, `fiction`, `blog`, `draft`, `tone`, `style`, `rewrite`, `summarize`, `translate`, `explain` |
| `search` | `search`, `find`, `lookup`, `research`, `browse`, `fetch`, `retrieve`, `news`, `latest`, `current`, `today`, `web` |
| `data` | `data`, `csv`, `json`, `sql`, `query`, `database`, `table`, `chart`, `plot`, `analyze`, `dataset`, `pipeline`, `etl`, `schema` |

Matching is case-insensitive, whole-word. If the prompt matches keywords from multiple domains, the domain with the most keyword hits wins.

### Matching Logic

```
for each domain in semantic_model_map:
    count keyword hits in prompt
if any domain has hits:
    pick domain with highest hit count
    return semantic_model_map[domain]
else:
    fall through to tier routing
```

### Example

Prompt: `"Fix the bug in the authentication function and add unit tests"`

- `code` hits: `bug`, `function`, `test` → 3 hits
- `creative` hits: none
- `math` hits: none

Result: routes to `semantic_model_map["code"]` → `qwen-coder`.

### Configuring Semantic Routing

```bash
curl -X PATCH http://localhost:8080/api/runtimes/<runtime-id> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "semantic_model_map": {
      "code": "qwen-coder",
      "math": "deepseek-r1"
    }
  }'
```

Only domains with a model configured are active. An empty or absent `semantic_model_map` skips semantic routing entirely and falls through to tier routing.

### Future Enhancement: Embedding-Based Semantic Routing

**Current implementation uses hardcoded keywords. This section documents a future enhancement for contextual understanding.**

#### Motivation

Hardcoded keywords are fast (0ms), deterministic, and handle 80% of cases well. However, they have limitations:

- **Brittle matching** - Misses variations like "create function" vs "implement functionality"
- **Limited nuance** - Can't distinguish "write a poem" (creative) from "write code" (code)
- **Multi-domain ambiguity** - "Write tests for the math solver" matches both `code` and `math`
- **No adaptation** - Can't learn from user patterns or language evolution

#### Proposed Hybrid Architecture

```go
type SemanticRouter struct {
    keywordMatcher  *KeywordMatcher  // Fast path (current implementation)
    embeddingRouter *EmbeddingRouter // Slow path (future enhancement)
}

func (s *SemanticRouter) Route(prompt string) (domain string, confidence float64) {
    // Try keyword matching first (0ms)
    domain, confidence := s.keywordMatcher.Match(prompt)
    
    // If high confidence (3+ keyword hits), use it immediately
    if confidence >= 0.7 {
        return domain, confidence
    }
    
    // If low confidence and embedding router configured, use semantic matching
    if confidence > 0 && confidence < 0.7 && s.embeddingRouter != nil {
        semanticDomain, semanticConf := s.embeddingRouter.Match(prompt)
        if semanticConf > confidence {
            return semanticDomain, semanticConf
        }
    }
    
    // Fallback to keyword result or empty (proceed to tier routing)
    return domain, confidence
}
```

#### Configuration

Add optional `embedding_model` field to runtime config to enable semantic layer:

```json
{
  "semantic_model_map": {
    "code": "qwen-coder",
    "math": "deepseek-r1"
  },
  "embedding_model": "text-embedding-3-small",  // Optional: enables semantic fallback
  "embedding_threshold": 0.75                   // Optional: minimum confidence to use semantic
}
```

#### Implementation Considerations

**Benefits:**
- Contextual understanding catches edge cases
- Handles linguistic variations naturally
- Better multi-domain resolution
- Adapts as language evolves

**Tradeoffs:**
- Adds 50-200ms latency per routing decision
- Small cost per classification (~$0.0001/1K tokens for embeddings)
- Requires embedding model availability (local or API)
- Less transparent debugging ("why did this route here?")
- Dependency risk if embedding service is unavailable

#### Rollout Strategy

1. **Phase 1 (Current)**: Hardcoded keywords only - fast, simple, good enough for MVP
2. **Phase 2**: Add embedding infrastructure with opt-in flag per runtime
3. **Phase 3**: Collect metrics on keyword vs semantic agreement/disagreement
4. **Phase 4**: Auto-enable semantic for ambiguous cases based on confidence thresholds

#### Success Metrics

Track before enabling by default:
- Keyword match confidence distribution
- Multi-domain collision rate
- User corrections/overrides of routing decisions
- Latency impact on task initiation
- Cost per routing decision

**Decision**: Start with keywords. Add semantic only when data shows clear misclassification patterns that justify the latency/cost tradeoffs.

---

## Tier Routing

### Tiers

Each runtime has a `tier_model_map` (stored as JSONB on the `agent_runtime` table) that maps complexity tiers to model IDs:

```json
{
  "simple":   "gpt-4o-mini",
  "standard": "gpt-4o",
  "heavy":    "claude-opus-4-5"
}
```

#### How tier assignment works

The model catalog (`ModelEntry`) returned by the daemon carries only `id`, `label`, `provider`, `default`, `thinking`, and `service_tiers`. There is no cost or capability-level field. So Cerebra uses a two-level strategy:

**Level 1 — Explicit map (primary, always correct)**

The user sets `tier_model_map` on the runtime via the API. Cerebra uses it as-is. This is the only fully correct approach because the user knows their account, their API keys, and which models they actually have access to.

**Level 2 — Name-pattern inference (fallback, best-effort)**

If `tier_model_map` is not set, Cerebra infers the tier from the model ID string:

| Pattern in model ID | Inferred tier |
|---|---|
| `mini`, `haiku`, `flash`, `nano`, `small`, `lite` | `simple` |
| `opus`, `large`, `ultra`, `max`, `plus`, `pro` | `heavy` |
| anything else | `standard` |

This works for most provider naming conventions (OpenAI, Anthropic, Google) but is fragile for custom or fine-tuned model IDs. A pattern miss silently falls to `standard`.

**Level 3 — No routing**

If neither the explicit map nor name inference produces a model, Cerebra returns empty and the daemon uses the static model configured on the agent. No task is ever failed due to a routing miss.

### Complexity Scoring

`cerebra/scorer.go` scores the prompt and returns one of three tiers:

| Tier | When |
|---|---|
| `simple` | Short prompt, no complex keywords |
| `standard` | Medium length or contains keywords like `fix`, `add`, `update`, `test`, `debug` |
| `heavy` | Long prompt or contains keywords like `refactor`, `architect`, `design`, `migrate` |

Scoring is heuristic — token count (word-split) + keyword detection. No LLM call, no latency.

### Routing Logic

`cerebra/router.go` takes the tier + the runtime's `tier_model_map` + the unavailability cache and returns `(runtimeID, modelID)`:

1. Score the prompt → get tier
2. Look up `tier_model_map[tier]` for the primary runtime
3. If that model is marked unavailable → scan other runtimes for the same model ID (cross-runtime fallback)
4. If no fallback exists → return empty string (daemon uses the static model as-is)

### Note on Overlap with Semantic Routing

Semantic routing and tier routing are independent passes. A `code` domain match routes to `qwen-coder` regardless of whether the prompt is simple or heavy. Tier routing only runs when semantic routing finds no match.

### Session Affinity

`cerebra/session.go` prevents mid-conversation model switches from breaking context:

- On first turn: router picks the model, stores it as `session_model` on the issue/chat_session row
- On follow-up turns: if the new tier is equal or lower, reuse `session_model` (same model, same context)
- If the new tier escalates (e.g. simple → heavy): update `session_model` to the stronger model

### Unavailability Cache

`cerebra/unavailability.go` tracks models that have hit quota or rate limits:

- In-memory + DB-backed (`cerebra_model_unavailability` table)
- `MarkUnavailable(runtimeID, model, ttl)` — called after a task fails with a quota signal
- `IsAvailable(runtimeID, model) bool` — checked by the router before selecting a model
- Default TTL: 1 hour

### Log Parser

`cerebra/log_parser.go` scans runtime output lines after a task completes and detects quota exhaustion signals:

| Signal | Action |
|---|---|
| `insufficient_quota` | Mark model unavailable |
| `rate_limit_exceeded` | Mark model unavailable |
| `quota exceeded` | Mark model unavailable |
| `context_length_exceeded` | **No action** — this is a context error, not a quota error |

The distinction matters: context length errors should not blacklist a model.

---

## File Map

### New Files

| File | Purpose |
|---|---|
| `server/internal/cerebra/semantic.go` | Hardcoded keyword sets + domain matching logic |
| `server/internal/cerebra/scorer.go` | Heuristic complexity scorer → `simple \| standard \| heavy` |
| `server/internal/cerebra/router.go` | Full router: semantic pass → tier pass → fallback |
| `server/internal/cerebra/session.go` | Session affinity — reads/writes `session_model` |
| `server/internal/cerebra/unavailability.go` | In-memory + DB unavailability cache |
| `server/internal/cerebra/log_parser.go` | Post-task log scanner for quota signals |
| `server/internal/cerebra/semantic_test.go` | Unit tests for keyword matching and domain resolution |
| `server/internal/cerebra/scorer_test.go` | Unit tests for tier scoring |
| `server/internal/cerebra/router_test.go` | Unit tests for routing + fallback + unavailability |
| `server/internal/cerebra/log_parser_test.go` | Unit tests distinguishing quota vs context errors |
| `server/migrations/398_cerebra_runtime_tier_model_map.up.sql` | Adds `tier_model_map JSONB` to `agent_runtime` |
| `server/migrations/398_cerebra_runtime_tier_model_map.down.sql` | Rollback |
| `server/migrations/399_cerebra_session_model.up.sql` | Adds `session_model TEXT` to `issues` and `chat_sessions` |
| `server/migrations/399_cerebra_session_model.down.sql` | Rollback |
| `server/migrations/400_cerebra_model_unavailability.up.sql` | Creates `cerebra_model_unavailability` table |
| `server/migrations/400_cerebra_model_unavailability.down.sql` | Rollback |
| `server/pkg/db/queries/cerebra.sql` | sqlc queries for all Cerebra DB operations |

### Modified Files

| File | Change |
|---|---|
| `server/internal/daemon/daemon.go` | Wire `cerebra.Router` into `Daemon` struct; call `Route()` in `runTask` before `ExecOptions` is built |
| `server/internal/daemon/execenv/execenv.go` | Add `Model string` field to `ExecOptions` (already exists — no change needed) |
| `server/internal/handler/runtime.go` | Add API endpoint to set/update `tier_model_map` and `semantic_model_map` on a runtime |

---

## DB Schema

### `agent_runtime` (modified)

```sql
ALTER TABLE agent_runtime
  ADD COLUMN tier_model_map     JSONB,
  ADD COLUMN semantic_model_map JSONB;
```

`tier_model_map` valid shape: `{"simple": "<model_id>", "standard": "<model_id>", "heavy": "<model_id>"}`. All three keys are optional.

`semantic_model_map` valid shape: `{"<domain>": "<model_id>", ...}`. Valid domain keys are `code`, `math`, `creative`, `search`, `data`. Missing domains are simply not matched.

### `issues` and `chat_sessions` (modified)

```sql
ALTER TABLE issues ADD COLUMN session_model TEXT;
ALTER TABLE chat_sessions ADD COLUMN session_model TEXT;
```

Stores the model chosen for the first turn of a conversation. Cleared when the issue is closed.

### `cerebra_model_unavailability` (new)

```sql
CREATE TABLE cerebra_model_unavailability (
  runtime_id  UUID        NOT NULL,
  model       TEXT        NOT NULL,
  marked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  ttl_seconds INT         NOT NULL DEFAULT 3600,
  PRIMARY KEY (runtime_id, model)
);
```

---

## sqlc Queries (`cerebra.sql`)

```sql
-- GetRuntimeTierModelMap
-- GetIssueSessionModel
-- SetIssueSessionModel
-- GetChatSessionModel
-- SetChatSessionModel
-- GetAllRuntimeTierModelMaps   ← for cross-runtime fallback
-- UpsertModelUnavailability
-- DeleteModelUnavailability
-- GetActiveModelUnavailabilities
```

---

## API Endpoint

`PATCH /api/runtimes/:id` — extended to accept `tier_model_map`:

```bash
curl -X PATCH http://localhost:8080/api/runtimes/<runtime-id> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "tier_model_map": {
      "simple":   "gpt-4o-mini",
      "standard": "gpt-4o",
      "heavy":    "claude-opus-4-5"
    }
  }'
```

Validation: keys must be `simple`, `standard`, or `heavy`; values must be non-empty strings.

---

## Testing via CLI (No Frontend Needed)

### 1. Configure the runtime

```bash
curl -X PATCH http://localhost:8080/api/runtimes/<runtime-id> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "tier_model_map": {
      "simple":   "gpt-4o-mini",
      "standard": "gpt-4o",
      "heavy":    "claude-opus-4-5"
    },
    "semantic_model_map": {
      "code": "qwen-coder",
      "math": "deepseek-r1"
    }
  }'
```

### 2. Fire a semantic-match task

```bash
multica issue create \
  --title "Fix the bug in the authentication function and add unit tests" \
  --assignee "YourAgent"
```

Expected: semantic router matches `code` domain (hits: `bug`, `function`, `test`) → routes to `qwen-coder`.

### 3. Fire a simple-tier task (no semantic match)

```bash
multica issue create --title "Fix typo in README" --assignee "YourAgent"
```

Expected: no semantic match → tier scorer returns `simple` → routes to `gpt-4o-mini`.

### 4. Fire a heavy-tier task (no semantic match)

```bash
multica issue create \
  --title "Refactor the entire authentication architecture to support multi-tenant OAuth2 with PKCE" \
  --assignee "YourAgent"
```

Expected: daemon routes to `claude-opus-4-5`.

### 5. Observe routing decisions

```bash
multica daemon logs -f
```

### 6. Check which model was used

```bash
multica issue runs <issue-id>
multica issue run-messages <task-id>
```

### 7. Test session affinity (follow-up on same issue)

```bash
multica issue comment add <issue-id> --content "Also migrate the session store to Redis"
```

Expected: same model as the first turn (session affinity holds).

### 8. Test unavailability fallback

Simulate quota exhaustion by checking daemon logs for `insufficient_quota` or `rate_limit_exceeded`. The router should automatically mark the model unavailable and route the next task to the fallback model.

---

## Build Order

Build in phases to keep each step within context limits. Each phase is self-contained and testable before moving to the next.

---

### Phase 1 — Migrations + DB Queries + Cerebra Package

**All new files. No existing files need to be read or modified.**

**Migrations (6 files):**
- `server/migrations/398_cerebra_runtime_tier_model_map.up.sql` — adds `tier_model_map JSONB` and `semantic_model_map JSONB` to `agent_runtime`
- `server/migrations/398_cerebra_runtime_tier_model_map.down.sql`
- `server/migrations/399_cerebra_session_model.up.sql` — adds `session_model TEXT` to `issues` and `chat_sessions`
- `server/migrations/399_cerebra_session_model.down.sql`
- `server/migrations/400_cerebra_model_unavailability.up.sql` — creates `cerebra_model_unavailability` table
- `server/migrations/400_cerebra_model_unavailability.down.sql`

**DB queries (1 file):**
- `server/pkg/db/queries/cerebra.sql` — all sqlc queries: `GetRuntimeTierModelMap`, `GetRuntimeSemanticModelMap`, `GetAllRuntimeTierModelMaps`, `GetIssueSessionModel`, `SetIssueSessionModel`, `GetChatSessionModel`, `SetChatSessionModel`, `UpsertModelUnavailability`, `DeleteModelUnavailability`, `GetActiveModelUnavailabilities`

**Cerebra package (9 files):**
- `server/internal/cerebra/semantic.go` — hardcoded keyword sets + domain matching
- `server/internal/cerebra/scorer.go` — complexity scorer returning `simple | standard | heavy`
- `server/internal/cerebra/router.go` — full two-pass router: semantic → tier → fallback
- `server/internal/cerebra/session.go` — session affinity reads/writes
- `server/internal/cerebra/unavailability.go` — in-memory + DB unavailability cache
- `server/internal/cerebra/log_parser.go` — post-task quota signal detection
- `server/internal/cerebra/semantic_test.go`
- `server/internal/cerebra/scorer_test.go`
- `server/internal/cerebra/router_test.go`
- `server/internal/cerebra/log_parser_test.go`

**Verify:** run `go build ./server/internal/cerebra/...` — should compile cleanly with no daemon changes yet.

---

### Phase 2 — Daemon Wiring (`daemon.go`)

**One file modified. Read only two targeted sections, not the full 9k-line file.**

Sections to read:
- `New()` function — to find where the `Daemon` struct is initialized and wire in `cerebra.Router`
- `runTask()` around line 7389 — the model resolution block where `Route()` is called

Changes:
- Add `cerebra *cerebra.Router` field to the `Daemon` struct
- Initialize it in `New()` with DB access and the unavailability cache
- In `runTask()`, insert the `Route()` call after the static model is resolved and before `resolveTaskModelSelection()` is called
- After task completion, call `cerebra.LogParser.Parse(logs)` to detect quota signals

**Verify:** `go build ./server/...` — daemon compiles with router wired in.

---

### Phase 3 — Handler (`runtime.go`)

**One file modified. Read only the runtime update handler section.**

Changes:
- Extend the `PATCH /api/runtimes/:id` handler to accept `tier_model_map` and `semantic_model_map` fields
- Validate `tier_model_map` keys are one of `simple`, `standard`, `heavy`
- Validate `semantic_model_map` keys are one of `code`, `math`, `creative`, `search`, `data`
- Persist both via the new sqlc queries from Phase 1

**Verify:** curl the endpoint with a valid and invalid payload, confirm 200 and 400 responses respectively.

---

### Phase 4 — End-to-End Test via CLI

No code changes. Validate the full routing pipeline using the CLI test scenarios documented above.

1. Run migrations: `make migrate` or equivalent
2. Configure runtime with both maps via curl
3. Fire semantic-match issues and confirm correct model in daemon logs
4. Fire tier-only issues and confirm tier model selection
5. Confirm session affinity holds on follow-up comments
6. Confirm unavailability fallback triggers after a quota signal

---

## Key Invariants

- **Never fail a task due to routing.** If the router errors or returns empty, the daemon falls back to the static model silently.
- **Semantic routing wins over tier routing.** A domain match always takes priority regardless of prompt complexity.
- **Keyword matching is hardcoded, model assignment is user-configured.** The keyword sets are built into `semantic.go` and never change at runtime. Only the domain→model mapping is user-controlled via `semantic_model_map`.
- **Context errors ≠ quota errors.** `context_length_exceeded` must never blacklist a model.
- **Session affinity only escalates, never de-escalates mid-conversation.** A heavy task cannot downgrade a session that started on a standard model.
- **Cross-runtime fallback is read-only.** The router may select a model from a different runtime's map, but it never reassigns the task's runtime — only the model string changes.
