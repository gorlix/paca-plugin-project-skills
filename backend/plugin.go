package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/google/uuid"
)

// pacaNamePrefix and reservedTriggerPrefix mirror the naming convention
// enforced by services/api at plugin-install time for the `skills.names`
// manifest field (see docs/plugins/skills-plugin-system.md in Paca-AI/paca):
// every Agent Skill in the Paca ecosystem is named `paca-<slug>`, and the
// `paca-trigger-` sub-namespace is reserved for Paca's own fixed
// per-conversation scaffolding skills.
//
// This plugin cannot register its skills through that manifest mechanism —
// `skills.baseUrl`/`skills.names` is resolved once, at plugin install time,
// against a tarball extracted to disk by services/api's Installer, and a
// WASM plugin has no filesystem access at runtime to keep that store in
// sync with skills a project creates or edits live (see this plugin's
// README for the full explanation). Applying the same `paca-` naming
// convention here anyway keeps a project's skills consistent with every
// other skill surfaced in the ecosystem, and keeps the door open for a
// future host capability that could bridge the two.
const (
	pacaNamePrefix        = "paca-"
	reservedTriggerPrefix = "paca-trigger-"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// skillColumns lists project_skills' columns in the exact physical order
// Postgres reports them in after migrations/0001-0003: 0003 dropped the
// original `body` column and appended `doc_id` at the end, so the position
// of every column below matches the table's *current* shape, not creation
// order. Every SELECT against this table below uses this full, fixed column
// list — not because Postgres needs it (a real SELECT projects exactly the
// columns you name), but because plugintest's InMemoryDB mock always
// returns a matched row in the table's physical column order regardless of
// what the SELECT clause asked for. Selecting (and indexing into results)
// in this same order keeps positional access correct under both the mock
// and the real database.
const skillColumns = "id, project_id, name, description, triggers, created_by, created_at, updated_at, doc_id"

const (
	colID = iota
	colProjectID
	colName
	colDescription
	colTriggers
	colCreatedBy
	colCreatedAt
	colUpdatedAt
	colDocID
)

type projectSkillsPlugin struct {
	db  *plugin.DB
	kv  *plugin.KV
	log *plugin.Logger
}

func (p *projectSkillsPlugin) Init(ctx *plugin.Context) error {
	p.db = ctx.DB()
	p.kv = ctx.KV()
	p.log = ctx.Log()

	// Routes embed :projectId (rather than just /skills) because the host's
	// plugin proxy only resolves req.Caller.ProjectID when the manifest's
	// requirePermissions(scope=project) route path contains a :projectId
	// path segment — confirmed against services/api's ProxyRequest handler
	// (internal/transport/http/handler/plugin_handler.go's projectMemberParam),
	// which looks up that exact param name (default "projectId") in the
	// matched route's path params. Omitting it silently leaves
	// Caller.ProjectID empty on every request.
	ctx.Route("GET", "/projects/:projectId/skills", p.listSkills)
	ctx.Route("POST", "/projects/:projectId/skills", p.createSkill)
	ctx.Route("GET", "/projects/:projectId/skills/:name", p.getSkill)
	ctx.Route("PATCH", "/projects/:projectId/skills/:name", p.updateSkill)
	ctx.Route("DELETE", "/projects/:projectId/skills/:name", p.deleteSkill)
	ctx.Route("GET", "/projects/:projectId/skills-folder", p.getSkillsFolder)
	ctx.Route("POST", "/projects/:projectId/skills-folder", p.setSkillsFolder)

	p.log.Info("com.gorlix.project-skills initialized")
	return nil
}

func (p *projectSkillsPlugin) Shutdown() {
	p.log.Info("com.gorlix.project-skills shutdown")
}

type successEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type skillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
	DocID       string   `json:"doc_id"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type skillDetail struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
	DocID       string   `json:"doc_id"`
	Content     string   `json:"content"`
}

func (p *projectSkillsPlugin) listSkills(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	rows, err := p.db.Query(
		"SELECT "+skillColumns+" FROM project_skills WHERE project_id = $1 ORDER BY name",
		projectID,
	)
	if err != nil {
		p.log.Error("listSkills query failed: " + err.Error())
		res.Error(500, "failed to list skills")
		return
	}

	summaries := make([]skillSummary, 0, len(rows.Rows))
	for _, row := range rows.Rows {
		summaries = append(summaries, skillSummary{
			Name:        toString(row, colName),
			Description: toString(row, colDescription),
			Triggers:    decodeTriggers(toString(row, colTriggers)),
			DocID:       toString(row, colDocID),
			CreatedAt:   toString(row, colCreatedAt),
			UpdatedAt:   toString(row, colUpdatedAt),
		})
	}

	res.JSON(200, successEnvelope{Success: true, Data: summaries})
}

func (p *projectSkillsPlugin) createSkill(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	type createBody struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Triggers    []string `json:"triggers"`
		DocID       string   `json:"doc_id"`
	}

	payload, err := plugin.JSONBody[createBody](req)
	if err != nil {
		res.Error(400, "invalid JSON body")
		return
	}

	name, err := normalizeName(payload.Name)
	if err != nil {
		res.Error(400, err.Error())
		return
	}
	description := strings.TrimSpace(payload.Description)
	if description == "" {
		res.Error(400, "description is required")
		return
	}
	docID := strings.TrimSpace(payload.DocID)
	if docID == "" {
		res.Error(400, "doc_id is required")
		return
	}

	// The body lives entirely in that Document — never authored by this
	// plugin — so the only thing worth checking here is that it's a real,
	// non-deleted document belonging to this same project (read-only access
	// to the host's own `documents` table; see backend/README or the
	// runtime's sensitiveTableColumns/coreSensitiveFields for why an
	// unqualified SELECT against a core table is permitted for plugins).
	docCheck, err := p.db.Query(
		"SELECT 1 FROM documents WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL",
		docID, projectID,
	)
	if err != nil {
		p.log.Error("createSkill doc check failed: " + err.Error())
		res.Error(500, "failed to create skill")
		return
	}
	if len(docCheck.Rows) == 0 {
		res.Error(400, "doc_id does not refer to an existing document in this project")
		return
	}

	existing, err := p.db.Query(
		"SELECT 1 FROM project_skills WHERE project_id = $1 AND name = $2",
		projectID, name,
	)
	if err != nil {
		p.log.Error("createSkill existence check failed: " + err.Error())
		res.Error(500, "failed to create skill")
		return
	}
	if len(existing.Rows) > 0 {
		res.Error(409, "a skill named '"+name+"' already exists in this project")
		return
	}

	triggersJSON, _ := json.Marshal(payload.Triggers)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = p.db.Exec(
		`INSERT INTO project_skills (id, project_id, name, description, triggers, created_by, created_at, updated_at, doc_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.NewString(), projectID, name, description, string(triggersJSON),
		req.Caller.CallerID, now, now, docID,
	)
	if err != nil {
		p.log.Error("createSkill insert failed: " + err.Error())
		res.Error(500, "failed to create skill")
		return
	}

	content, err := p.renderSkillContent(name, description, payload.Triggers, docID)
	if err != nil {
		p.log.Error("createSkill content render failed: " + err.Error())
	}
	res.JSON(201, successEnvelope{Success: true, Data: skillDetail{
		Name:        name,
		Description: description,
		Triggers:    payload.Triggers,
		DocID:       docID,
		Content:     content,
	}})
}

// documentsColumns/colDocContent mirror skillColumns/colX above: like
// plugintest's InMemoryDB mock, this SELECT names its full physical column
// list and indexes results positionally rather than trusting the SELECT
// clause to control which columns come back.
const documentsColumns = "id, project_id, content, deleted_at"
const colDocContent = 2

// renderSkillContent fetches the linked document's current block content
// and converts it to a full SKILL.md (frontmatter + markdown body).
func (p *projectSkillsPlugin) renderSkillContent(name, description string, triggers []string, docID string) (string, error) {
	rows, err := p.db.Query("SELECT "+documentsColumns+" FROM documents WHERE id = $1 AND deleted_at IS NULL", docID)
	if err != nil {
		return renderSkillMD(name, description, triggers, ""), err
	}
	if len(rows.Rows) == 0 {
		return renderSkillMD(name, description, triggers, ""), nil
	}
	var raw []byte
	switch v := rows.Rows[0][colDocContent].(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		if v != nil {
			b, _ := json.Marshal(v)
			raw = b
		}
	}
	return renderSkillMD(name, description, triggers, blocksToMarkdown(raw)), nil
}

func (p *projectSkillsPlugin) getSkill(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	name := req.PathParam("name")
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	rows, err := p.db.Query(
		"SELECT "+skillColumns+" FROM project_skills WHERE project_id = $1 AND name = $2",
		projectID, name,
	)
	if err != nil {
		p.log.Error("getSkill query failed: " + err.Error())
		res.Error(500, "failed to read skill")
		return
	}
	if len(rows.Rows) == 0 {
		res.Error(404, "skill not found")
		return
	}

	row := rows.Rows[0]
	triggers := decodeTriggers(toString(row, colTriggers))
	docID := toString(row, colDocID)
	content, err := p.renderSkillContent(toString(row, colName), toString(row, colDescription), triggers, docID)
	if err != nil {
		p.log.Error("getSkill content render failed: " + err.Error())
	}
	res.JSON(200, successEnvelope{Success: true, Data: skillDetail{
		Name:        toString(row, colName),
		Description: toString(row, colDescription),
		Triggers:    triggers,
		DocID:       docID,
		Content:     content,
	}})
}

func (p *projectSkillsPlugin) updateSkill(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	name := req.PathParam("name")
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	type updateBody struct {
		Description string   `json:"description"`
		Triggers    []string `json:"triggers"`
	}

	payload, err := plugin.JSONBody[updateBody](req)
	if err != nil {
		res.Error(400, "invalid JSON body")
		return
	}
	description := strings.TrimSpace(payload.Description)
	if description == "" {
		res.Error(400, "description is required")
		return
	}

	triggersJSON, _ := json.Marshal(payload.Triggers)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// doc_id is immutable once set (same "no rename after creation" rule as
	// name) — the body itself is edited entirely through the host's own
	// Documentation page, never through this route.
	rows, err := p.db.Query(
		"SELECT "+skillColumns+" FROM project_skills WHERE project_id = $1 AND name = $2",
		projectID, name,
	)
	if err != nil {
		p.log.Error("updateSkill lookup failed: " + err.Error())
		res.Error(500, "failed to update skill")
		return
	}
	if len(rows.Rows) == 0 {
		res.Error(404, "skill not found")
		return
	}
	docID := toString(rows.Rows[0], colDocID)

	rowsUpdated, err := p.db.Exec(
		`UPDATE project_skills SET description = $1, triggers = $2, updated_at = $3
		 WHERE project_id = $4 AND name = $5`,
		description, string(triggersJSON), now, projectID, name,
	)
	if err != nil {
		p.log.Error("updateSkill failed: " + err.Error())
		res.Error(500, "failed to update skill")
		return
	}
	if rowsUpdated == 0 {
		res.Error(404, "skill not found")
		return
	}

	content, err := p.renderSkillContent(name, description, payload.Triggers, docID)
	if err != nil {
		p.log.Error("updateSkill content render failed: " + err.Error())
	}
	res.JSON(200, successEnvelope{Success: true, Data: skillDetail{
		Name:        name,
		Description: description,
		Triggers:    payload.Triggers,
		DocID:       docID,
		Content:     content,
	}})
}

// deleteSkill removes only this plugin's own project_skills row — the linked
// Document is left untouched. It's a first-class Document in its own right
// (visible and editable in the Documentation feature independent of this
// plugin), so deleting the skill link shouldn't silently delete a document
// the user may still want.
func (p *projectSkillsPlugin) deleteSkill(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	name := req.PathParam("name")
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	rowsDeleted, err := p.db.Exec(
		"DELETE FROM project_skills WHERE project_id = $1 AND name = $2",
		projectID, name,
	)
	if err != nil {
		p.log.Error("deleteSkill failed: " + err.Error())
		res.Error(500, "failed to delete skill")
		return
	}
	if rowsDeleted == 0 {
		res.Error(404, "skill not found")
		return
	}

	res.NoContent()
}

// skillsFolderKVKey namespaces the KV entry per project — this plugin's KV
// store is shared across every route, not scoped by path param the way the
// DB schema is scoped by project_id column.
func skillsFolderKVKey(projectID string) string {
	return "skills-folder:" + projectID
}

// getSkillsFolder returns the doc_folder_id this project's "new skill"
// button should file documents under, if one has been recorded. Frontend
// still confirms the folder still exists (not deleted) before trusting
// this — the plugin has no way to be notified of a Document folder
// deletion happening entirely on the host's side.
//
// This exists so the frontend never has to *discover* the right folder by
// matching on its name: an earlier version did that (find-or-create a
// root-level folder literally named "Skills"), which silently adopted any
// unrelated folder a project happened to already have with that exact
// name — the Documentation folder API enforces no name uniqueness at all,
// confirmed by creating two sibling folders named "Skills" in the same
// project without error. Persisting the real ID here, once, at creation
// time makes reuse deterministic instead of name-based guessing.
func (p *projectSkillsPlugin) getSkillsFolder(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}
	folderID, _ := p.kv.Get(skillsFolderKVKey(projectID))
	res.JSON(200, successEnvelope{Success: true, Data: map[string]string{"doc_folder_id": folderID}})
}

// setSkillsFolder records the doc_folder_id for this project, called once
// by the frontend right after it creates that folder for the first time.
func (p *projectSkillsPlugin) setSkillsFolder(req *plugin.Request, res *plugin.Response) {
	projectID := req.Caller.ProjectID
	if projectID == "" {
		res.Error(400, "missing project scope")
		return
	}

	type setBody struct {
		DocFolderID string `json:"doc_folder_id"`
	}
	payload, err := plugin.JSONBody[setBody](req)
	if err != nil {
		res.Error(400, "invalid JSON body")
		return
	}
	docFolderID := strings.TrimSpace(payload.DocFolderID)
	if docFolderID == "" {
		res.Error(400, "doc_folder_id is required")
		return
	}

	p.kv.Set(skillsFolderKVKey(projectID), docFolderID)
	res.NoContent()
}

// normalizeName validates the user-supplied skill name and applies the
// ecosystem-wide `paca-` prefix convention (see the package-level comment
// on pacaNamePrefix for why this can't instead be done by declaring the
// name in the plugin manifest's `skills.names`).
func normalizeName(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", errString("name is required")
	}
	if !slugPattern.MatchString(strings.TrimPrefix(slug, pacaNamePrefix)) {
		return "", errString("name must be lowercase alphanumeric segments joined by single hyphens (e.g. 'new-docker-service')")
	}

	name := slug
	if !strings.HasPrefix(name, pacaNamePrefix) {
		name = pacaNamePrefix + name
	}
	if strings.HasPrefix(name, reservedTriggerPrefix) {
		return "", errString("the 'paca-trigger-' prefix is reserved for Paca's own scaffolding skills")
	}
	return name, nil
}

// renderSkillMD produces a complete AgentSkills-format SKILL.md document
// (YAML frontmatter + markdown body) from structured fields. Building it
// server-side, rather than accepting raw SKILL.md text from the client,
// guarantees every skill this plugin stores has valid, well-formed
// frontmatter.
func renderSkillMD(name, description string, triggers []string, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + yamlQuote(name) + "\n")
	b.WriteString("description: " + yamlQuote(description) + "\n")
	if len(triggers) > 0 {
		b.WriteString("triggers:\n")
		for _, t := range triggers {
			b.WriteString("  - " + yamlQuote(t) + "\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func decodeTriggers(raw string) []string {
	if raw == "" {
		return nil
	}
	var triggers []string
	if err := json.Unmarshal([]byte(raw), &triggers); err != nil {
		return nil
	}
	return triggers
}

func toString(row []any, idx int) string {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return ""
	}
	return fmt.Sprint(row[idx])
}

type errString string

func (e errString) Error() string { return string(e) }
