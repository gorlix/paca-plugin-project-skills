# com.gorlix.project-skills — Project Skills

A Paca plugin that lets a project register its own [Agent Skills](https://agentskills.io/specification)
(the `SKILL.md` format) and serve them over HTTP, so any project member or
compliant client tooling can list, create, edit, and delete skills that are
specific to that project — without touching Paca's core.

Implements point 3 of [Paca-AI/paca#453](https://github.com/Paca-AI/paca/issues/453)
("a Paca project can register its own skills"), as scoped by the maintainer's
reply on that issue.

## Why this is a separate plugin, and what it deliberately does *not* do

Issue #453 also asked for two other things: (1) non-lossy distribution of
skills to `agentskills.io`-compliant clients, and (2) this plugin. Those are
different concerns — the first is a change to `scripts/install-paca-skills.sh`
in the `Paca-AI/paca` monorepo, unrelated to plugin code — so this repo only
covers (2), per `CONTRIBUTING.md`'s "one concern per PR" guidance.

### The `paca-` naming convention, and why `skills.baseUrl` isn't wired up

Every Agent Skill in the Paca ecosystem is named `paca-<slug>` (see
`docs/plugins/skills-plugin-system.md` in `Paca-AI/paca`), and a plugin
declares its bundled skills via a `skills.baseUrl` + `skills.names` section
in `plugin.json`. This plugin applies that same naming convention to every
skill it manages (see `normalizeName` in `backend/plugin.go`: a name is
lowercased, validated as a slug, and has `paca-` prepended if missing), but
**does not** declare a `skills` section in its own manifest, because that
mechanism cannot serve what this plugin manages:

- `skills.baseUrl` is resolved once, at plugin install/update time, against
  a `skills_tar_gz_url` tarball that `services/api`'s Installer extracts to
  a local directory on the host (`{PLUGINS_SKILLS_DIR}/{pluginId}/`). The
  `names` list is declared explicitly in the manifest because the static
  file server behind `baseUrl` doesn't support directory listing.
- A WASM backend plugin (this one included) has **no filesystem access** at
  runtime — it's the security model documented in `docs/plugins/overview.md`.
  There is no way for this plugin to add a new file under that directory, or
  update `plugin.json`'s `names` list, when a project creates or edits a
  skill live.

In short: `skills.baseUrl` is for a plugin's own static, versioned,
install-time skill bundle (see `paca-plugin-example`'s
`skills/paca-hello-greeting/`) — not for skills a project's members create
and edit at runtime. This plugin's skills are instead stored in its own
database table and served from its own `/api/v1/plugins/com.gorlix.project-skills/skills`
routes (see below). A client that wants these skills on disk in
`agentskills.io` layout (e.g. to drop them into a `.claude/skills/` folder)
needs to call this plugin's routes directly — it is a separate integration
from `scripts/install-paca-skills.sh`, which only reads `GET /api/v1/skills`
(Paca's own bundled skills + skills wired through `skills.baseUrl`).

This also means: skills registered through this plugin are **not** currently
injected into `agent-runner` conversations. That gap is unrelated to this
plugin — as of writing, `agent-runner` doesn't merge *any* plugin's
`skills.baseUrl` skills into a conversation either (see the gap notice at
the top of `docs/plugins/skills-plugin-system.md`).

## API

All routes are scoped to the calling project (`req.Caller.ProjectID`) and
namespaced under `/api/v1/plugins/com.gorlix.project-skills/`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/skills` | List the project's skills (name, description, triggers, timestamps). |
| `POST` | `/skills` | Create a skill. Body: `{ "name", "description", "triggers"?, "body" }`. |
| `GET` | `/skills/:name` | Fetch one skill, including its rendered `SKILL.md` content. |
| `PATCH` | `/skills/:name` | Replace a skill's `description`, `triggers`, and `body`. |
| `DELETE` | `/skills/:name` | Delete a skill. |

`name` is normalized server-side: lowercased, validated as
lowercase-alphanumeric segments joined by single hyphens, and prefixed with
`paca-` if not already present (so `POST {"name": "new-docker-service", ...}`
is stored and returned as `paca-new-docker-service`). Names starting with
`paca-trigger-` are rejected — that sub-namespace is reserved for Paca's own
scaffolding skills.

`GET /skills/:name` and the create/update responses include a `content`
field: a complete, server-rendered `SKILL.md` document (YAML frontmatter +
markdown body), built from the structured fields rather than accepted as raw
text from the client — this guarantees every stored skill has valid
frontmatter.

## Development

```bash
cd backend
go test ./...

# TinyGo (preferred — see Paca-AI/paca's developer guide for why):
tinygo build -target=wasip1 -buildmode=c-shared -o ../dist/project-skills.wasm .

# Standard Go fallback:
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../dist/project-skills.wasm .
```

See `plugin_test.go` for unit tests using the SDK's `plugintest` package —
no running Paca instance required.

To install locally against a dev Paca instance, follow the "Install the
Plugin Locally" step in `Paca-AI/paca`'s
[Plugin Developer Guide](https://github.com/Paca-AI/paca/blob/main/docs/plugins/developer-guide.md#step-4--install-the-plugin-locally).

## Structure

```text
paca-plugin-project-skills/
  plugin.json
  backend/
    main.go              ← WASM entry point
    plugin.go            ← route handlers, naming/rendering logic
    plugin_test.go
    go.mod
    migrations/
      0001_create_project_skills.sql
```

No frontend or MCP surface in this first version — see the "Future work"
section of the issue discussion for `mcp.tools` as a possible follow-up
(exposing `list_project_skills` / `get_project_skill` as MCP tools so an AI
client can read them without raw HTTP).
