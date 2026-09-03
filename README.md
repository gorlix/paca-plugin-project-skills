# com.gorlix.project-skills — Project Skills

A Paca plugin that lets a project register its own [Agent Skills](https://agentskills.io/specification)
(the `SKILL.md` format), specific to that project, without touching Paca's
core. A skill's metadata (name, description, triggers) lives in this plugin's
own table; its instructions are a real Documentation entry, edited in Paca's
own editor. Skills are reachable three ways: the project sidebar (create/
open/delete), raw HTTP (`GET`/`POST`/`PATCH`/`DELETE`, for any
`agentskills.io`-compliant client), and two read-only MCP tools for AI
clients connected to Paca.

Implements point 3 of [Paca-AI/paca#453](https://github.com/Paca-AI/paca/issues/453)
("a Paca project can register its own skills"), as scoped by the maintainer's
reply on that issue.

## Installing

Not yet in the official `Paca-AI/paca-plugins` catalog — a
[draft PR](https://github.com/Paca-AI/paca-plugins/pull/31) is open, held in
draft pending the host-side fix noted under "Frontend" below (without it,
the sidebar section installs but renders empty on any instance that doesn't
have that fix). Until then:

- **Manual install** against a dev instance: the "Install the Plugin
  Locally" step in `Paca-AI/paca`'s
  [Plugin Developer Guide](https://github.com/Paca-AI/paca/blob/main/docs/plugins/developer-guide.md#step-4--install-the-plugin-locally).
- **Real marketplace install flow, on your own fork of the catalog**: point
  your instance's `PLUGINS_MARKETPLACE_CATALOG_URL` at a
  `raw.githubusercontent.com` URL of a catalog `plugins.json` containing
  this plugin's entry (see the draft PR above for the exact entry shape),
  then use the normal `POST /api/v1/admin/plugins/marketplace/install` flow.
  This is how every release since v0.2.0 has actually been tested — real
  tarball download, real migration run, real WASM load, not a shortcut.

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
database table and served from its own `/api/v1/plugins/com.gorlix.project-skills/projects/:projectId/skills`
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

The manifest declares every route with a `:projectId` path segment
(`/projects/:projectId/skills[...]`, not just `/skills[...]`) — this is
required for the host to resolve `Caller.ProjectID` at all. Confirmed by
reading `services/api`'s plugin proxy (`ProxyRequest` → `projectMemberParam`
in `internal/transport/http/handler/plugin_handler.go`): it only populates
project scope when a `requirePermissions(scope=project)` route's path
contains that segment. An earlier version of this plugin used bare `/skills`
paths and silently got an empty `Caller.ProjectID` on every call — caught by
testing against a real running instance, not by unit tests (which construct
`Caller` directly and never exercise this resolution step).

| Method | Path | Description |
|---|---|---|
| `GET` | `/projects/:projectId/skills` | List the project's skills (name, description, triggers, `doc_id`, timestamps). |
| `POST` | `/projects/:projectId/skills` | Create a skill. Body: `{ "name", "description", "triggers"?, "doc_id" }`. |
| `GET` | `/projects/:projectId/skills/:name` | Fetch one skill: rendered `SKILL.md` content plus any `references/`/`scripts/` files (see below). |
| `PATCH` | `/projects/:projectId/skills/:name` | Update a skill's `description` and `triggers` (`doc_id` is immutable once set). |
| `DELETE` | `/projects/:projectId/skills/:name` | Delete a skill (leaves its linked Document untouched — see below). |

`name` is normalized server-side: lowercased, validated as
lowercase-alphanumeric segments joined by single hyphens, and prefixed with
`paca-` if not already present (so `POST {"name": "new-docker-service", ...}`
is stored and returned as `paca-new-docker-service`). Names starting with
`paca-trigger-` are rejected — that sub-namespace is reserved for Paca's own
scaffolding skills.

### A skill's body is a real project Document, not text this plugin owns

`doc_id` must reference an existing, non-deleted Document in the same
project (validated with a read-only lookup against the host's own
`documents` table — permitted for plugin backends since `documents` has no
`coreSensitiveFields` declared; see `services/api`'s plugin runtime). The
skill's actual instructions are whatever that Document's content is, edited
through Paca's own Documentation feature (real BlockNote editor) — this
plugin never renders a body editor of its own.

`GET /projects/:projectId/skills/:name` (and the create/update responses)
include a `content` field: a complete `SKILL.md` document (YAML frontmatter,
built from the structured fields, plus the linked Document's content
converted from BlockNote block JSON to markdown by `backend/markdown.go`) —
this is what an external `agentskills.io`-compliant client or the MCP tools
below actually consume.

## Frontend

Two components (Module Federation, `frontend/`): a `sidebar.project.section`
skill tree matching the Documentation section's own look, and a
`project.page` (hidden from the auto-generated Plugins nav — the sidebar
section already provides navigation) with a small name/description/triggers
form and an "Open SKILL.md" button that navigates straight into the host's
native Documentation editor. No custom rich-text editor here — see "A
skill's body is a real project Document" above for why.

> This currently **requires an unreleased host fix**: stock Paca-AI/paca
> doesn't inject `api`/`ui`/`meta` props into `sidebar.project.section`
> components at all (only `project.page` gets them, via
> `usePluginBaseProps` in the routed page). Without that fix this plugin's
> sidebar section renders empty. Not yet upstreamed.

## MCP tools

Two read-only tools (`mcp/`, loaded by Paca's MCP server as a plain Node ESM
module — see `docs/plugins/mcp-plugin-system.md` in `Paca-AI/paca`), so an
AI client can discover and read a project's skills without raw HTTP:

| Tool | Description |
|---|---|
| `project_skills_list` | List a project's skills (name, description, triggers). |
| `project_skills_get` | Fetch one skill's full `SKILL.md` content by name. |

Both simply call this plugin's own `GET /projects/:projectId/skills[...]`
routes via `PluginAPIClient` — no separate backend logic. Verified against a
real running instance by driving the actual MCP server binary
(`apps/mcp/build/index.js`) over stdio with real JSON-RPC `initialize` /
`tools/call` messages, not just a direct function-call test.

## Testing

- `go test -race ./...` — 76.4% coverage as of this writing, including every
  handler's project-scope guard, field validation, and error paths. Weakest
  spot: `markdown.go`'s less common block/mark branches (nested lists,
  checklists, links, italic/strike) are exercised by only a few cases so far.
- `golangci-lint run` — clean.
- Verified against a real `docker compose -f deploy/docker-compose.dev.yml`
  Paca instance: installed via the manual dev flow (copy `backend.wasm` +
  `plugin.json` into `plugins/local/backend/com.gorlix.project-skills/`,
  insert the `plugins` DB row, restart `services/api`), then a full CRUD
  cycle over real HTTP (create with auto `paca-` prefix, 409 on duplicate,
  400 on a `paca-trigger-` name, list, get, patch, delete, 404 after delete)
  against the real Postgres-backed plugin schema
  (`plugin_data_com_gorlix_project_skills.project_skills`). Also confirmed
  empirically that a skill created here does not appear in
  `GET /api/v1/skills`, matching the `skills.baseUrl` boundary explained
  above. This live pass is what caught the `:projectId` routing bug noted
  in the API section — unit tests alone did not.

## Development

```bash
cd backend
go test ./...

# TinyGo (preferred — see Paca-AI/paca's developer guide for why):
tinygo build -target=wasip1 -buildmode=c-shared -o backend.wasm .

# Standard Go fallback:
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o backend.wasm .
```

See `plugin_test.go` for unit tests using the SDK's `plugintest` package —
no running Paca instance required.

```bash
cd frontend   # or: cd mcp
npm install
npm run typecheck
npm run build     # → dist/ — remoteEntry.js + assets (frontend) or mcp.js (mcp)
npm run dev       # build --watch, for local iteration
```

To install locally against a dev Paca instance, follow the "Install the
Plugin Locally" step in `Paca-AI/paca`'s
[Plugin Developer Guide](https://github.com/Paca-AI/paca/blob/main/docs/plugins/developer-guide.md#step-4--install-the-plugin-locally) —
or install from the [marketplace catalog PR](#installing) above, which
exercises the exact same artifact-download path a real install would.

### Releasing

Push a `v*` tag and `.github/workflows/build-and-release.yml` builds all
four artifacts (TinyGo WASM, frontend bundle, MCP bundle, migrations) and
publishes them to a GitHub Release, matching what the marketplace catalog
entry's `artifacts.*_tar_gz_url` fields point to. No manual tarball-building
needed — this used to be a hand-run `tar` + `gh release create` sequence for
the first three tags (v0.2.0, v0.2.1) before this workflow existed.

## Structure

```text
paca-plugin-project-skills/
  plugin.json
  backend/
    main.go              ← WASM entry point
    plugin.go            ← route handlers, naming/rendering logic
    markdown.go          ← BlockNote block JSON → markdown, for SKILL.md rendering
    plugin_test.go
    go.mod
    migrations/
      0001_create_project_skills.sql
      0002_create_project_skill_files.sql   ← unused so far, see Future work
      0003_link_skill_to_document.sql
  frontend/
    src/
      ProjectSkillsSidebarSection.tsx   ← sidebar.project.section
      ProjectSkillsPage.tsx             ← project.page (metadata form)
      constants.ts
  mcp/
    src/index.ts          ← project_skills_list / project_skills_get
```

## Multi-file skills (references/, scripts/)

A skill can have `references/*` and `scripts/*` files alongside its
SKILL.md, stored in `project_skill_files` (migration 0002) — plain text
only (markdown, shell, code), edited via a small textarea in the frontend's
Files section, not Documentation: these aren't prose, and BlockNote isn't
the right tool for a shell script. `GET .../skills/:name` and the MCP
`project_skills_get` tool both return them inline alongside the SKILL.md
content, so a client gets the whole bundle in one call.

`assets/` (real binaries — images, PDFs) is deliberately **not**
supported: the plugin backend has no storage/blob access or outbound HTTP
(only DB + KV, see `plugin-sdk-go`'s `Context`), so it can never read the
bytes of a real file attachment even if one were uploaded through
Documentation's own attachment mechanism (those live in MinIO, reachable
only by an authenticated browser via a presigned URL). Text-only files
sidestep this entirely — no storage access needed, `project_skill_files` is
just another plugin-owned table.

Two routes, both POST (the frontend SDK's `PluginApiClient` has no PUT, and
no body-carrying DELETE — see the comments on `upsertSkillFile`/
`deleteSkillFile` in `backend/plugin.go` for why, including why a
`/files/*path`-style wildcard route isn't an option either):

| Method | Path | Description |
|---|---|---|
| `POST` | `/projects/:projectId/skills/:name/files` | Upsert `{path, content}`. `path` must match `(references\|scripts)/<name>` — one segment, no nesting, no `..`. |
| `POST` | `/projects/:projectId/skills/:name/files/delete` | Delete `{path}`. |

Deleting the skill cascades to its files (`ON DELETE CASCADE` on
`skill_id`) — verified against real Postgres, not just the unit test mock
(which doesn't enforce foreign keys).

## Future work

- **Client-side download integration**: `scripts/install-paca-skills.sh`
  in `Paca-AI/paca` only ever fetches Paca's bundled skills and
  `skills.baseUrl`-wired plugin skills (`GET /api/v1/skills`) — it has no
  awareness of this plugin's own per-project skills at all. A follow-up
  (either a flag on that script, or a small standalone tool) would call
  this plugin's `GET /projects/:projectId/skills` + `GET .../skills/:name`
  and write the results into the same `agentskills.io` folder layout that
  script now writes for bundled skills (see its own non-lossy rewrite),
  so a project's custom skills end up on disk the same way as Paca's
  built-in ones.
