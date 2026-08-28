Switched model to Claude Sonnet 4.5

---

Analyze this project and understand it throroughly. Mainly its functionality, UI and features. Do not waste tokens in build files or unnecesary files. Be token efficient.

---


Here's a thorough breakdown of the project:

---

## Multica — Project Overview

**Multica** (Multiplexed Information and Computing Agent) is a full-stack **human-AI agent collaboration platform** — essentially a project management tool where AI agents are first-class teammates alongside humans.

---

## Architecture

- **Backend**: Go (`server/`) — Chi router, sqlc, gorilla/websocket, PostgreSQL
- **Frontend**: Next.js App Router (`apps/web/`) + Electron desktop (`apps/desktop/`) + React Native mobile (`apps/mobile/`)
- **Monorepo**: pnpm workspaces + Turborepo
- **Packages**:
  - `packages/core/` — headless business logic (React Query + Zustand, zero DOM)
  - `packages/ui/` — atomic UI components (shadcn/Base UI, zero business logic)
  - `packages/views/` — shared business pages/components
- **State**: React Query for server state, Zustand for client/view state
- **Realtime**: WebSocket hub with Redis relay

---

## Core Features

### Issues
The central entity. Issues have:
- Status, priority, assignee (human or agent), labels, due/start dates, custom properties
- Sub-issues with stage grouping and progress rings
- Parent-child hierarchy
- **4 views**: List, Board (kanban), Table, Gantt
- Inline editing everywhere (title, description, all properties)
- Virtualized timeline (Virtuoso) with comments, activity log, reactions
- Thread resolution, reply threads, thread minimap rail
- Deep-link to specific comments (inbox notifications)
- In-page find (Cmd+F)
- Revision conflict detection for concurrent edits
- Batch actions toolbar

### Agents
AI agents are assignable like humans. They:
- Get assigned issues, run tasks, report progress
- Have runtimes (OpenClaw, OpenCode, Hermes, Claude — the "Cerebra" router)
- Show live activity indicators on issues
- Have skills (YAML-defined capabilities)
- Support MCP (Model Context Protocol) for tool use
- Can form **Squads** (multi-agent teams with a leader agent)

### Chat
Two-pane IM-style interface:
- Thread list (left) + conversation (right), resizable panels
- Floating FAB chat window (also available)
- URL-addressable sessions (`?session=<id>`)
- `?agent=<id>` deep link to start a new chat with a specific agent
- Quick actions (AI-suggested follow-ups)
- Project context attachment
- Queue for messages sent while agent is busy
- Compact (mobile) mode: list/conversation toggle

### Autopilots
Automated triggers — cron-based or webhook-based — that fire agent tasks automatically.

### Inbox
Notification center with unread counts, cross-workspace unread dots on the workspace switcher.

### Projects & Squads
- Projects group issues with resources and local directory hints
- Squads are multi-agent configurations with a leader + workers

### Runtimes & Skills
- Runtimes bind agents to execution environments (local daemon or cloud)
- Skills are YAML packages that extend agent capabilities
- Runtime health monitoring, model catalog, local skills management

---

## UI Structure

### Sidebar (AppSidebar)
Three nav groups:
- **Personal**: Inbox (unread count), Chat (unread count), My Issues
- **Workspace**: Issues, Projects, Autopilots, Agents, Squads, Usage
- **Configure**: Runtimes, Skills, Settings

Plus:
- Workspace switcher dropdown (with pending invitations, cross-workspace unread dots)
- Pinned items (issues, projects, views) — drag-to-reorder via dnd-kit
- New Issue button with keyboard shortcut
- Help launcher + Discord card in footer

### Issue Detail
Two-panel layout (resizable):
- **Left**: Title editor, description (ProseMirror), sub-issues list, activity/comments timeline
- **Right sidebar**: Properties (status, assignee, project + optional: priority, stage, dates, labels, custom), quick actions, plugin panel, parent issue, pull requests, execution log, details, metadata

### Chat Page
Two-panel (resizable on desktop, stacked on mobile):
- Thread list with archive support
- Conversation with message list, banners (offline/archived/no-runtime), queue, input

---

## Integrations
- **VCS**: GitHub, GitLab, Forgejo (PR sidebar, webhooks)
- **Messaging**: Slack, DingTalk, Lark/Feishu, Telegram, WeCom
- **Tools**: Composio (external tool integrations for agents)
- **Auth**: JWT, PAT, CloudFront, OAuth callbacks

---

## i18n
Full internationalization: English, Japanese, Korean, Simplified Chinese across all views.

---

Okay I am planning to implement a smart dynamic model router. Any agent can pick a runtime and see for available models. Then based on a pre-calculated complexity score the prompt will be routed to its corresponding tier model. Like a simple prompt prompt must be given to a simple model while a heavy reasoning prompt must be given to a heavy model, while maintaining session affinity so that context is not lost. Can this be done?

---


Yes, this is absolutely doable and fits naturally into the existing architecture. Let me map out exactly where everything plugs in.

## What already exists

- **Runtimes** — already bound to agents, have a model catalog (`runtime_model_catalog.go`, `runtime_models.go`)
- **Tasks** — the unit of work sent to an agent's runtime
- **Daemon** — the local process that actually invokes the runtime (OpenClaw, Hermes, etc.)
- **`execenv/runtime_config.go`** — runtime config delivered to the daemon per-task

## How it fits together

The router sits between **task creation** and **daemon claim**. Here's the flow:

```
User prompt
    ↓
[Complexity Scorer]  ← analyzes prompt features
    ↓
tier: simple | standard | heavy
    ↓
[Model Router]  ← maps tier → model from runtime's catalog
    ↓
runtime_config injected into task
    ↓
Daemon picks up task, uses the routed model
    ↓
Session affinity: subsequent tasks in same issue/chat inherit the model
```

---

## The 3 layers to build

### 1. Complexity Scorer (server-side, Go)
Lives in a new `server/internal/modelrouter/` package.

Scoring signals (no LLM needed, pure heuristics):
- Prompt token estimate
- Presence of keywords: "reason", "analyze", "compare", "architecture", "debug", "refactor" → heavy
- Code block count / length
- Number of referenced issues/files in context
- Whether it's a follow-up reply (lower base score)

```go
type Tier string
const (
    TierSimple   Tier = "simple"    // e.g. gpt-4o-mini, claude-haiku
    TierStandard Tier = "standard"  // e.g. gpt-4o, claude-sonnet
    TierHeavy    Tier = "heavy"     // e.g. o3, claude-opus
)

func ScorePrompt(prompt string, contextTokens int) Tier
```

### 2. Model Router (server-side, Go)
Maps tier → concrete model from the runtime's catalog. The runtime owner configures which model sits at each tier — stored as a new `tier_model_map` JSON column on the runtime record.

```go
type TierModelMap struct {
    Simple   string `json:"simple"`
    Standard string `json:"standard"`
    Heavy    string `json:"heavy"`
}

func RouteModel(tier Tier, catalog []Model, tierMap TierModelMap) string
```

This runs inside `handler/daemon.go` at task claim time, injecting the chosen model into the `runtime_config` before it's delivered to the daemon.

### 3. Session Affinity (server-side, Go)
The key constraint: once a model is chosen for a session (issue thread or chat session), subsequent tasks in that session **inherit** the same model unless the score jumps a full tier AND the user explicitly allows escalation.

Storage: a `session_model` column on the task's issue or chat session, written on first task, read on subsequent ones.

```go
// On task creation:
if existingModel := getSessionModel(issueId); existingModel != "" {
    // use existing unless score escalates AND escalation is allowed
    model = maybeEscalate(existingModel, scoredTier, tierMap)
} else {
    model = RouteModel(scoredTier, catalog, tierMap)
    setSessionModel(issueId, model)
}
```

---

## UI changes needed

### Runtime settings page — Tier configuration
In `packages/views/runtimes/components/`, add a tier mapping editor:

```
Simple tier    → [model picker dropdown]
Standard tier  → [model picker dropdown]  
Heavy tier     → [model picker dropdown]
```

### Issue/Chat header — Model indicator chip
A small read-only chip showing the current session's active model (reuses the existing `IssueAgentHeaderChip` slot pattern). Optionally a "reset session model" button.

### Agent settings — Router toggle
Per-agent opt-in: "Enable smart model routing" toggle, so agents that need a fixed model aren't affected.

---

## What needs to be added to the DB

```sql
-- On runtimes table
ALTER TABLE runtimes ADD COLUMN tier_model_map JSONB;

-- Session affinity tracking
ALTER TABLE issues ADD COLUMN session_model TEXT;
-- For chat:
ALTER TABLE chat_sessions ADD COLUMN session_model TEXT;
```

Both as separate `CREATE INDEX CONCURRENTLY` migration files per the repo's hard rules.

---

## Complexity

| Layer | Effort | Risk |
|---|---|---|
| Complexity scorer | Low — pure heuristics | Low |
| Model router | Low — maps tier → model | Low |
| Session affinity | Medium — concurrent task claims need a lock | Medium |
| Runtime tier config UI | Low — reuses existing model picker | Low |
| Model indicator chip | Low — reuses existing chip pattern | Low |

The concurrent claim race (two tasks for the same issue claimed simultaneously) is the only real risk — it's solvable with a `SELECT ... FOR UPDATE` on the session model row, which the existing scheduler already does for task claims.

---

## What you do NOT need to build

- A separate LLM call to score prompts (heuristics are sufficient and faster)
- A new runtime type (the router is a layer on top of existing runtimes)
- Changes to the daemon (it already accepts a model in runtime_config)

Want me to start implementing any specific layer?

---

First answer a few questions. First, The router here may deicide which model to choose how will it be ensured that the runtime (like openclaw, opencode, etc) will use that particular model?

---


Great question. Let me trace exactly how model selection flows from the server to the runtime today.

---


Now I have a complete picture. Here's the direct answer:

---

## How the router ensures the runtime uses the chosen model

There are **two distinct mechanisms** depending on the runtime type, and they work very differently.

### Mechanism 1: Model is passed as a CLI argument / env var (Claude, Codex, Hermes, etc.)

The daemon spawns the runtime CLI as a subprocess. The model is passed directly as a flag or environment variable. For example:
- `claude --model claude-opus-4-5 ...`
- `codex --model o3 ...`

This is handled in `server/internal/daemon/daemon.go` when building the `exec.Cmd`. The router just needs to **set the model field on the task's runtime config**, which the daemon reads when constructing the subprocess command.

### Mechanism 2: OpenClaw — model is embedded in the per-task config file

OpenClaw is more complex. The daemon writes a **per-task `openclaw-config.json`** (via `prepareOpenclawConfig` in `openclaw_config.go`) and sets `OPENCLAW_CONFIG_PATH` to point at it. OpenClaw reads this file to know which agent/model to use.

The `buildPerTaskOpenclawConfig` function assembles this JSON. It currently rewrites `agents.defaults.workspace` and `agents.list[].workspace` — but **not the model**. To inject a model, the router would need to also rewrite `agents.list[].model` (or `agents.defaults.model`) in that same JSON wrapper.

### Mechanism 3: AGENTS.md / CLAUDE.md brief injection

`InjectRuntimeConfig` writes a brief into the workdir (e.g. `AGENTS.md`, `CLAUDE.md`). This is **instructions text**, not model selection — it cannot force a model. Model selection must go through the CLI/config path above.

---

## So concretely, where does the router inject the model?

There are **two injection points**:

**Point A — `TaskContextForEnv` / daemon subprocess args** (Claude, Codex, Hermes, Reasonix, etc.)

The daemon reads the task's `runtime_config` from the claim payload and passes `--model <id>` to the CLI. The router writes the chosen model into the task's `runtime_config.model` field **before the task is claimed by the daemon**. The daemon already has code to pass this through — you just need to populate it.

**Point B — `buildPerTaskOpenclawConfig`** (OpenClaw only)

The router needs to add one extra field to the wrapper JSON:
```go
// inside buildPerTaskOpenclawConfig, after setting workspace:
if model != "" {
    agents["defaults"].(map[string]any)["model"] = model
    // also rewrite per-agent entries if needed
}
```

This is the **only** place OpenClaw reads its model — it does not accept a CLI flag for it.

---

## Summary

| Runtime | How model is enforced | Where router injects |
|---|---|---|
| Claude, Codex, Hermes, Reasonix, etc. | `--model` CLI flag on subprocess | `runtime_config.model` on the task claim |
| OpenClaw | `agents.defaults.model` in `openclaw-config.json` | `buildPerTaskOpenclawConfig` in `openclaw_config.go` |
| Hermes | `--model` or env var | `runtime_config.model` on the task claim |

The router does **not** need to touch `AGENTS.md`/`CLAUDE.md` at all — those are instruction briefs, not model selectors. The two injection points above are the complete and sufficient surface.

---

Compact chat history?

---


Compacting your chat history, this may take a moment.

---

You have reached the limit. 

Request ID: a926799b-c621-4603-a0e3-367ceafa6135 