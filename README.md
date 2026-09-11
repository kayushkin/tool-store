# tool-store

A registry of tools that can be seeded into AI coding agents and harnesses. Written in Go, with a small SQLite-backed HTTP service.

AI agents need tools — shell, file editing, search, browsers, custom CLIs, MCP servers. Each harness (Claude Code, Codex, Aider, etc.) configures these differently. tool-store provides one place to register what's available, what each tool's input schema is, and how it's invoked. Harnesses and orchestrators read from tool-store on session bring-up and provision themselves.

Every component is part of the [llm-bridge](https://github.com/kayushkin/llm-bridge) ecosystem. Use it standalone, or as a store inside [llm-bridge-server](https://github.com/kayushkin/llm-bridge-server).

## How it works

```
  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐
    Your Application  (dashboard, agent, harness)
  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┬ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘
                            │ HTTP (REST + JSON)
  ╔═════════════════════════╪═════════════════════════════╗
  ║                  tool-store                           ║
  ║                         │                             ║
  ║   ┌─────────────────────▼───────────────────────┐     ║
  ║   │           HTTP service (:8302)              │     ║
  ║   │                                             │     ║
  ║   │  Registry: list, upsert, enable/disable,    │     ║
  ║   │    fetch by id or name, fetch spec          │     ║
  ║   │                                             │     ║
  ║   │  Discovery: GET /locals                     │     ║
  ║   │  Invocation: POST /tools/by-name/{n}/invoke │     ║
  ║   └─────────────────────┬───────────────────────┘     ║
  ║                         │                             ║
  ║   ┌─────────────────────▼───────────────────────┐     ║
  ║   │              SQLite store                   │     ║
  ║   │   ~/.config/tool-store/tool-store.db        │     ║
  ║   └─────────────────────┬───────────────────────┘     ║
  ║                         │                             ║
  ║   ┌─────────────────────▼───────────────────────┐     ║
  ║   │          In-process tool registry           │     ║
  ║   │   Go-native impls registered via init()     │     ║
  ║   │   (shell, browser, web_search, fs, …)       │     ║
  ║   └─────────────────────────────────────────────┘     ║
  ╚═══════════════════════════════════════════════════════╝
```

A tool registered in tool-store can be one of three **kinds**, each with a different execution path:

| Kind | What it is | How tool-store handles it |
|------|------------|---------------------------|
| `local` | A Go function compiled into the tool-store binary | Invoked in-process via `POST /tools/by-name/{name}/invoke` |
| `cli`   | An arbitrary command + argv template | tool-store substitutes `{{var}}` placeholders from the input JSON, execs the binary, and returns stdout |
| `mcp`   | An external MCP server (stdio / http / sse) | Not invoked here — clients fetch the launcher spec via `/tools/by-name/{name}/spec` and spawn it themselves (e.g. Claude Code via `--mcp-config`) |

tool-store seeds its own registry on startup with every in-process tool the binary ships, but every newly seeded row starts **disabled**. Operators (or eventually a UI) explicitly enable a tool when they want it. Existing rows preserve whatever enabled state they have, so a deliberate enable or disable survives restarts. Discovery via `GET /locals` shows what's available regardless of registration state.

## What you get

| Capability | Description |
|------------|-------------|
| **Unified registry** | One source of truth for tools across harnesses, regardless of kind |
| **Multi-kind dispatch** | Local Go, arbitrary CLI, and external MCP all sit under one schema |
| **Safe templating** | `{{var}}` substitution in CLI argv with no shell expansion — no injection vector |
| **Discovery** | `GET /locals` lists every in-process tool the binary ships, ready to enable |
| **Idempotent CRUD** | Upsert by name, enable/disable, list with kind/tag/query filters |
| **No coupling to providers** | Schemas are plain JSON Schema — no Anthropic SDK or other LLM types in the wire format |
| **Single source of credentials** | Tools declare `env_keys`, never values — secrets resolve at provision time from your existing credential store |

## Packages

### `toolstore` — Registry types and HTTP handlers

```go
import toolstore "github.com/kayushkin/tool-store"
```

- `Tool`, `Kind` (`mcp` | `cli` | `local`), `MCPSpec`, `CLISpec`, `LocalSpec`
- `Open(dataDir) (*Store, error)` — opens the SQLite-backed store
- `UpsertTool`, `GetTool`, `GetToolByName`, `ListTools`, `DeleteTool`, `SetEnabled`
- `RegisterHandlers(mux, store, HandlerOptions)` — wires the REST API onto an `*http.ServeMux`
- `HandlerOptions` — pluggable callbacks: `InvokeLocal`, `ListLocals`. Both optional.

### `schema` — JSON Schema helpers

```go
import "github.com/kayushkin/tool-store/schema"
```

A no-dependency builder for tool input schemas. `Props(required, properties)` returns an `InputSchema` that JSON-marshals to standard `{"type":"object","properties":{...},"required":[...]}` — accepted as-is by Anthropic, OpenAI, Google, and MCP. Helpers: `Str`, `Integer`, `Bool`, `Array`. `Parse[T]` unmarshals an input string into a typed struct.

### `tools` — In-process Go tool implementations

```go
import "github.com/kayushkin/tool-store/tools"
```

The Go-native tool implementations that ship inside `tool-store`. Each is an `Impl{ Name, Description, InputSchema, Run }`. They register themselves via `init()`. Consumers call `Register`, `ByName`, `All`.

| Tool | Description |
|------|-------------|
| `shell_commands` | Run one or more bash commands |
| `read_files`, `write_files`, `edit_files`, `list_files` | File operations |
| `ripgrep` | Pattern search via ripgrep |
| `web_search` | Brave Search API |
| `web_fetch` | Fetch URL and extract readable text |
| `browser` | Browser automation via PinchTab |
| `scheduler` | Manage cron jobs via the scheduler service |
| `end_turn` | Signal end of an agent turn |

Context-bearing tools (`recent_files`, `repo_map`, `scratchpad`, `task_plan`) are exposed as factories rather than auto-registered — they need a per-instance `repoRoot` / `agentName` and are typically wired from above (e.g. by [llm-bridge-server](https://github.com/kayushkin/llm-bridge-server) at session bring-up).

### `cmd/tool-store` — HTTP service

```bash
go build ./cmd/tool-store
./tool-store
# or via systemd (see deploy.sh)
```

Starts an HTTP server (default `:8302`) backed by SQLite at `~/.config/tool-store/tool-store.db`. Wires the `tools` package into `RegisterHandlers` so local tools are invokable in-process and discoverable via `GET /locals`.

Environment:
- `TOOL_STORE_ADDR` — listen address (default `:8302`)
- `TOOL_STORE_DATA_DIR` — data directory (default `~/.config/tool-store`)

## HTTP API

```
GET    /health
GET    /tools                                  ?kind=&tag=&q=&enabled=true&limit=
POST   /tools                                  body: full Tool JSON (upsert by name)
GET    /tools/{id}
DELETE /tools/{id}
POST   /tools/{id}/enable
POST   /tools/{id}/disable
GET    /tools/by-name/{name}
GET    /tools/by-name/{name}/spec              CLI/MCP launcher spec (for harnesses that run tools themselves)
POST   /tools/by-name/{name}/invoke            body: input JSON; only valid for kind=local or kind=cli
GET    /locals                                 in-process registry (what's available to enable)

GET    /instances/{id}/tools                   tools opted-in for this harness instance
POST   /instances/{id}/tools/by-name/{name}    enable a tool for this instance
DELETE /instances/{id}/tools/by-name/{name}    disable a tool for this instance
GET    /tools/by-name/{name}/instances         every instance opted into this tool
```

### Per-instance opt-in

Tools have a global `enabled` flag and a per-instance opt-in. **Global is master**: a globally-disabled tool can't be opted in (the API returns 409). When a tool is globally enabled, no instance has it until you explicitly opt in:

```bash
# Enable globally
curl -s -X POST http://localhost:8302/tools/3/enable

# Opt instance "main-1" in
curl -s -X POST http://localhost:8302/instances/main-1/tools/by-name/brave-search

# What's enabled for this instance?
curl -s http://localhost:8302/instances/main-1/tools

# Where is brave-search active?
curl -s http://localhost:8302/tools/by-name/brave-search/instances
```

If a tool is globally re-disabled, its rows in `instance_tools` are preserved but `GET /instances/{id}/tools` filters them out — re-enabling globally restores the prior opt-ins.

## Quick start

### Discover what's available

```bash
curl -s http://localhost:8302/locals | jq '.[].name'
```

### Register a local tool

```bash
curl -s -X POST http://localhost:8302/tools -H 'content-type: application/json' -d '{
  "name": "shell_commands",
  "description": "Run one or more bash commands",
  "kind": "local",
  "local": {"symbol": "shell_commands"},
  "enabled": true
}'
```

### Register a CLI tool

```bash
curl -s -X POST http://localhost:8302/tools -H 'content-type: application/json' -d '{
  "name": "ls",
  "description": "List a directory",
  "kind": "cli",
  "cli": {
    "command": "ls",
    "args_template": ["-la", "{{path}}"],
    "timeout_ms": 5000
  },
  "input_schema": {"type":"object","properties":{"path":{"type":"string"}},"required":["path"]},
  "enabled": true
}'
```

### Register an MCP tool

```bash
curl -s -X POST http://localhost:8302/tools -H 'content-type: application/json' -d '{
  "name": "brave-search",
  "description": "Web search via Brave Search API",
  "kind": "mcp",
  "env_keys": ["BRAVE_API_KEY"],
  "mcp": {
    "transport": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-brave-search"]
  },
  "enabled": true
}'
```

### Invoke a tool

```bash
curl -s -X POST http://localhost:8302/tools/by-name/shell_commands/invoke \
  -H 'content-type: application/json' \
  -d '{"command":"echo hello"}'
# {"output":"hello\n"}
```

### Fetch a launcher spec for an MCP

```bash
curl -s http://localhost:8302/tools/by-name/brave-search/spec
# {"transport":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-brave-search"]}
```

## llm-bridge-server integration

When [llm-bridge-server](https://github.com/kayushkin/llm-bridge-server) creates a Claude Code session, it can opt the session into tool-store provisioning by passing a `tool_store_tools` field inside `HarnessConfig`:

```jsonc
POST /sessions
{
  "harness": "claudecode",
  "harness_config": {
    "tool_store_tools": ["brave-search", "playwright"]
  }
}
```

llm-bridge-server's `injectMCPConfig` step calls `POST :8302/provision`, writes the response to a tmpfile, replaces the field with `mcp_config: <path>`, and the claudecode harness picks the path up via its existing `--mcp-config` flag. No msg-type changes required — the contract rides through the opaque `HarnessConfig` blob. Setting both `tool_store_tools` and `mcp_config` is rejected (single source of truth).

A session that names no tools of its own gets whatever its **instance** has been opted into — the rows the Tools page writes through `POST /instances/{id}/tools/by-name/{name}`. llm-bridge-server asks for those by instance instead of by name:

```jsonc
POST /provision
{ "instance_id": "inst-cc-local" }
// → {"mcpServers": { … the instance's opted-in MCP tools … }}
```

A caller that already holds tool-store ids — llm-bridge-server, intersecting a principal's grant-store grants with an instance's opt-ins, has ids on both sides — names them by id instead:

```jsonc
POST /provision
{ "tool_ids": [13, 14] }
```

Exactly one of `tools`, `tool_ids` and `instance_id` is required; a request carrying more than one is rejected, because merging a standing preference with a per-call list gives the same field two sources of truth. They differ in how they treat a tool that is not an MCP server: `tools` and `tool_ids` name it outright, so a CLI or in-process local there is an error, while an instance's opt-in list legitimately spans every kind of tool, so the MCP subset is selected out of it. An instance nobody has ticked anything for provisions nothing and says so with `200 {}`, which is how a caller tells "no opt-ins" apart from "the lookup broke".

## Design principles

- **Disabled by default.** The registry seeds itself with every in-process tool, but every new row starts disabled. Enabling is the deliberate act — no tool is silently active.
- **Layers are transparent.** Input JSON, output strings, and launcher specs pass through unchanged. CLI substitution is literal — no shell, no quoting heuristics.
- **Single source of credentials.** Tools declare which env vars they need (`env_keys`); the values come from the canonical credential store at provision time, never from tool-store rows.
- **Three kinds, one shape.** `local`, `cli`, and `mcp` share the same `Tool` schema. Consumers branch on `kind` only when they have to.
- **Independent of any LLM SDK.** The schema is plain JSON Schema. Bring your own provider.
