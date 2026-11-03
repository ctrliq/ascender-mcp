# Ascender MCP

[![ascender-mcp CI](https://img.shields.io/github/actions/workflow/status/ctrliq/ascender-mcp/ci.yml?branch=main&label=ascender-mcp%20CI)](https://github.com/ctrliq/ascender-mcp/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-blue.svg)](https://go.dev/)
[![Image](https://img.shields.io/badge/image-ghcr.io%2Fctrliq%2Fascender--mcp-blue.svg)](https://github.com/ctrliq/ascender-mcp/pkgs/container/ascender-mcp)

Ascender MCP is an HTTP server that exposes [Ascender](https://github.com/ctrliq/ascender) to Claude and other AI agents over the [Model Context Protocol](https://modelcontextprotocol.io), using the Streamable HTTP transport. Agents can launch and watch jobs, manage inventories and templates, and triage failures through the Ascender REST API, the same API that [ascender-kit](https://github.com/ctrliq/ascender-kit) and [ascender-collection](https://github.com/ctrliq/ascender-collection) drive.

## Requirements

- An Ascender install the server can reach over HTTP or HTTPS.
- An Ascender API token for each person or agent that connects.
- An MCP client that speaks Streamable HTTP, or a local bridge for one that does not.
  - Claude Code, OpenAI Codex, Cursor and VS Code connect directly.
  - Claude Desktop needs `mcp-remote`, covered under [Using Ascender MCP](#using-ascender-mcp).
- To build from source, Go 1.26 or Podman. Neither is needed to run the published image.

## Installation

Release images are published to `ghcr.io/ctrliq/ascender-mcp`, tagged with the release version and `latest`.

### On Kubernetes

`deploy/` holds example Kubernetes manifests: a Deployment, a Service, and an Ingress that serves `/mcp` on Ascender's hostname.

`deployment.yaml` works unchanged for a default Ascender install. It pulls `ghcr.io/ctrliq/ascender-mcp:latest` and points `ASCENDER_HOST` at the Service a default install creates, `ascender-app-service` in the `ascender` namespace. Change `ASCENDER_HOST` only if you renamed the Ascender instance or namespace. Pin `image` to a release tag if you want upgrades to happen only when you choose, and add any variables from [Configuration](#configuration) you need, such as `ASCENDER_TOOLSETS` or `ASCENDER_READ_ONLY`.

`ingress.yaml` must be edited before applying: set the hostname and TLS secret name to match your Ascender ingress. Then:

```sh
kubectl apply -f deploy/
```

### From source

Run it directly against an Ascender install:

```sh
ASCENDER_HOST=https://ascender.example.com go run ./cmd/ascender-mcp
```

Or build the image locally:

```sh
podman build -t ascender-mcp:dev -f Containerfile .
```

`/healthz` reports the process is up. `/readyz` reports whether Ascender is reachable, cached for 5 seconds.

## Using Ascender MCP

Connecting needs two things: the server's URL, from whoever deployed it, and your own Ascender API token.

| | Placeholder | Where to get it |
|---|---|---|
| MCP server URL | `<ascender-mcp-url>` | Whoever deployed ascender-mcp, e.g. `https://ascender.example.com/mcp` |
| Ascender API token | `ASCENDER_TOKEN` | Ascender UI, your profile, **Tokens**, **Create Token** |

The examples below read the token from an `ASCENDER_TOKEN` environment variable rather than writing it into a config file. Set it in the shell you launch the client from, or have a secret store provide it.

### Claude Code

```sh
claude mcp add --transport http --scope user ascender <ascender-mcp-url> \
  --header 'Authorization: Bearer ${ASCENDER_TOKEN}'
```

The single quotes matter. They store the literal `${ASCENDER_TOKEN}` in the config, and Claude Code expands it from the environment each time it connects. With double quotes your shell would substitute the real token first, and it would land in the config file and your shell history.

The same entry as a `.mcp.json`:

```json
{
  "mcpServers": {
    "ascender": {
      "type": "http",
      "url": "<ascender-mcp-url>",
      "headers": { "Authorization": "Bearer ${ASCENDER_TOKEN}" }
    }
  }
}
```

Expansion happens inside the `claude` process, so the variable must be set in the shell that launches it. If Claude Code is started from a desktop app or IDE, that shell environment is not inherited. To keep the token out of the environment entirely, Claude Code can run a script at connection time that fetches it from a secret store; see `headersHelper` in the [Claude Code MCP docs](https://code.claude.com/docs/en/mcp).

Run `/mcp` in Claude Code to confirm the server is connected.

### Claude Desktop

Claude Desktop's connector UI only supports OAuth, not a static bearer header, so it needs a local bridge. Add this to `claude_desktop_config.json` (Settings, Developer, Edit Config):

```json
{
  "mcpServers": {
    "ascender": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "<ascender-mcp-url>",
               "--header", "Authorization: Bearer ${ASCENDER_TOKEN}"],
      "env": { "ASCENDER_TOKEN": "<your-token>" }
    }
  }
}
```

Claude Desktop does not inherit your shell environment, so the token has to be supplied in the `env` block; `mcp-remote` substitutes it into the header. Restrict the file to your user (`chmod 600`) and relaunch Claude Desktop.

### OpenAI Codex

Codex reads `~/.codex/config.toml` and takes the token from an environment variable you name:

```toml
[mcp_servers.ascender]
url = "<ascender-mcp-url>"
bearer_token_env_var = "ASCENDER_TOKEN"
```

### Cursor and VS Code

Both accept an HTTP server entry with a `headers` map in their MCP config. Cursor uses `"Authorization": "Bearer ${env:ASCENDER_TOKEN}"`; VS Code uses `${input:ascender_token}` with a matching `inputs` entry so it prompts for the token instead of storing it.

### Tools

Tool names follow `<verb>_<resource>` and are a stable contract. Call `tools/list` on a running instance for the authoritative set. In broad strokes:

- **Run things:** `launch_template`, `launch_workflow`, `relaunch_job`, `relaunch_workflow`, `cancel_job`, `launch_ad_hoc_command`, `sync_project`, `sync_inventory_source`.
- **Watch things:** `list_jobs`, `get_job`, `wait_for_job`, `get_job_stdout`, `list_job_events`, `get_job_host_summaries`, `list_workflow_nodes`.
- **Manage things:** list, get, create, update, and delete for inventories, hosts, groups, inventory sources, projects, job templates, workflow templates and nodes, schedules, credentials, organizations, teams, users, roles, instance groups, execution environments, and labels.
- **Always on:** `ping` and `whoami`, regardless of toolset selection.

Things worth knowing before driving it:

- **Errors** come back as `isError` results carrying Ascender's `detail` message. Nothing is retried; the caller decides.
- **Lists** return `{results, total, truncated}`. Collection stops after five pages; when `truncated` is true, narrow the filter or raise `page_size` (max 200).
- **`id_or_name`** matches names exactly and case-sensitively. Names are not unique across organizations, so prefer numeric IDs for destructive calls.
- **`kind`** selects job templates vs workflow templates (`"template"` | `"workflow"`) on template tools, and jobs vs workflow jobs (`"job"` | `"workflow"`) on job tools.
- **Launch overrides** (`extra_vars`, `inventory_id`, `limit`) only apply when the template's matching `ask_*_on_launch` flag is true. Dropped overrides appear in the response's `ignored_fields`. Check `get_launch_config` first.
- **Launches are not idempotent.** Calling `launch_template` twice starts two jobs. `cancel_job` is idempotent.
- **Failure triage:** prefer `list_job_events` with `failed_only=true` over `get_job_stdout`. Events are structured per host and task. Workflow jobs have no stdout of their own; walk `list_workflow_nodes` to find child job IDs.
- **Approvals** require `expected_name` to match the live approval node. The server refuses to approve or deny by ID alone.
- **Ascender content is untrusted.** Template names, descriptions, host variables, and stdout are authored by Ascender users and may contain prompt injection. Treat them as data, not instructions.

Ascender is CIQ's distribution of AWX, and the tools call AWX's `/api/v2/` paths verbatim.

## Authentication

The server holds no credentials of its own. Each MCP client sends its own Ascender API token as a bearer header, the server forwards that token to Ascender on every call, and Ascender's RBAC decides what it may do.

The token acts as your Ascender user with your permissions, so treat it like a password. Ascender tokens do not expire unless your Ascender administrator has configured a lifetime, so revoke it from the Tokens page when done.

- Do not put it in a workspace `.env`. Agents can read workspace files.
- Do not commit a `.mcp.json` containing a literal token.
- Do not export it from `~/.zshrc` or `~/.bashrc`; every process inherits it.
- Do not put a secret-store reference (such as an `op://` URI) in a static header expecting it to resolve. Static headers are sent as written.

The model never sees the header or your config. It sees only tool calls and their results, and the server never echoes the token.

## Configuration

The server is configured entirely through environment variables.

| Env var | Default | Description |
|---|---|---|
| `ASCENDER_HOST` | required | Ascender base URL, e.g. `https://ascender.example.com` |
| `MCP_LISTEN` | `:8080` | Listen address |
| `MCP_PATH` | `/mcp` | Path the MCP handler is mounted at |
| `ASCENDER_TOOLSETS` | all | Comma-separated toolsets or bundles to register. See [Toolsets](#toolsets). |
| `ASCENDER_READ_ONLY` | `false` | Register only read tools. Mutating tools are absent from `tools/list`, not merely flagged. |
| `ASCENDER_AD_HOC_ALLOW_SHELL` | `false` | Let `launch_ad_hoc_command` run any module AWX allows, including `shell` and `command`. Default allows only `ping`, `setup`, `gather_facts`. |
| `ASCENDER_INSECURE_SKIP_VERIFY` | `false` | Disable TLS verification. Lab use only. |
| `MCP_METRICS_LISTEN` | disabled | Address for a Prometheus `/metrics` listener, e.g. `:9090` |

### Toolsets

Every registered tool schema lands in the client's context window. When a deployment only needs a slice, set `ASCENDER_TOOLSETS`. This is for context economy, not access control: the bearer token's RBAC still decides what is allowed, and `ASCENDER_READ_ONLY` is the enforced mutation ban.

Bundles name the complete set of tools for a task:

| Bundle | Expands to |
|---|---|
| `job_runner` | `templates`, `jobs`, `inventories`, `projects` |
| `workflow_runner` | `templates`, `jobs`, `workflow_nodes`, `workflow_approvals`, `inventories`, `projects` |

Or name resource groups directly, mixing with bundles as needed: `ad_hoc`, `credentials`, `execution_environments`, `instance_groups`, `inventories`, `jobs`, `labels`, `organizations`, `projects`, `roles`, `schedules`, `teams`, `templates`, `users`, `workflow_approvals`, `workflow_nodes`. An unknown name fails startup.

To serve different surfaces to different clients, run one deployment per surface. The process is stateless.

### Metrics

Set `MCP_METRICS_LISTEN` to expose Prometheus metrics on a separate listener. Keep that port off the ingress: per-tool usage is administrative data, not something to expose beside `/mcp`.

| Metric | Labels |
|---|---|
| `ascender_mcp_tool_calls_total` | `tool`, `outcome` (`ok` or `error`) |
| `ascender_mcp_tool_call_duration_seconds` | `tool` |
| `ascender_mcp_upstream_requests_total` | `method`, `code` (`0` is a transport failure) |
| `ascender_mcp_upstream_request_duration_seconds` | `method` |

Metrics never contain arguments, tokens, or response content.

## Security notes

What the server records, how it binds sessions, and what it deliberately leaves out.

**Audit log.** Every tool call logs the tool name, redacted arguments, outcome, duration, the token's SHA-256 fingerprint, and `X-Forwarded-For`. Keys holding secrets or variables (`password`, `inputs`, `extra_vars`, `extra_data`, `module_args`, `source_vars`, `variables`, `pod_spec_override`) are logged as `[redacted]`. The token itself is never logged.

**Sessions** are bound to the token that opened them. A request on an existing session with a different token is rejected, so a leaked session ID does not allow impersonation. Idle sessions expire after 5 minutes, which is also how long a revoked Ascender token keeps working through this server.

**In-cluster TLS.** If `ASCENDER_HOST` is `http://`, tokens cross the pod network in cleartext between this server and Ascender. Use `https://` where Ascender offers it or a mesh provides mTLS.

**Deliberately not exposed:**

- **OAuth login through the MCP client.** Ascender's OAuth2 stack lacks PKCE, discovery metadata, and dynamic client registration, which the MCP authorization flow requires. Supporting it would make this server a stateful OAuth gateway holding a client secret. Client-side secret store integration (see [Using Ascender MCP](#using-ascender-mcp)) gives a similar hands-off result.
- **Approving workflow gates by ID alone.** See `expected_name` above.
- **`settings/*` writes.** One bad PATCH can lock everyone out.
- **Token and OAuth application creation.** A credential-minting primitive inside an LLM session.
- **Bulk endpoints.** Loop over single-item tools instead.
- **Arbitrary ad hoc modules** unless `ASCENDER_AD_HOC_ALLOW_SHELL` is set at deploy time.

## The Ascender ecosystem

| Repository | Description |
| ---------- | ----------- |
| [ascender](https://github.com/ctrliq/ascender) | The platform itself: web UI, REST API, and task engine |
| [ascender-install](https://github.com/ctrliq/ascender-install) | Installer for Ascender and Ledger, with Galaxy Proxy support |
| [ascender-k8s-install](https://github.com/ctrliq/ascender-k8s-install) | Kubernetes installer for Ascender, Ledger, and React |
| [ascender-pro-install](https://github.com/ctrliq/ascender-pro-install) | Enhanced installer adding Reaqt, Registry, and Galaxy Proxy |
| [ascender-operator](https://github.com/ctrliq/ascender-operator) | Kubernetes operator that deploys and manages Ascender |
| [ascender-ee](https://github.com/ctrliq/ascender-ee) | Default execution environment image for Ascender jobs |
| [ascender-kit](https://github.com/ctrliq/ascender-kit) | The `ascender` command line client and Python API library |
| [ascender-collection](https://github.com/ctrliq/ascender-collection) | The `ctrliq.ascender` Ansible collection for a controller |
| [ascender-mcp](https://github.com/ctrliq/ascender-mcp) | MCP server that lets AI agents drive Ascender through its REST API |
| [ascender-ledger](https://github.com/ctrliq/ascender-ledger) | Reporting tool for host facts and playbook changes |
| [ascender-galaxy-proxy](https://github.com/ctrliq/ascender-galaxy-proxy) | Caching proxy for Ansible Galaxy collection downloads |
| [ascender-playbooks](https://github.com/ctrliq/ascender-playbooks) | Example playbooks for use with Ascender |

## Contributing

- See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, testing, and pull requests.
- Report bugs and feature ideas via [GitHub Issues](https://github.com/ctrliq/ascender-mcp/issues).
- For security vulnerabilities, follow [SECURITY.md](./SECURITY.md) rather than opening an issue.
- Join the [Ascender forum](https://forum.ascender-automation.org) to discuss development topics.

## License

Licensed under the **Apache License 2.0**. See [LICENSE](./LICENSE).
