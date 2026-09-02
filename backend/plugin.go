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

// skillColumns lists project_skills' columns in the exact order the table
// was created in (see migrations/0001_create_project_skills.sql). Every
// SELECT against this table below uses this full, fixed column list — not
// because Postgres needs it (a real SELECT projects exactly the columns you
// name), but because plugintest's InMemoryDB mock always returns a matched
// row in the table's physical column order regardless of what the SELECT
// clause asked for. Selecting (and indexing into results) in this same
// order keeps positional access correct under both the mock and the real
// database.
const skillColumns = "id, project_id, name, description, triggers, body, created_by, created_at, updated_at"

const (
	colID = iota
	colProjectID
	colName
	colDescription
	colTriggers
	colBody
	colCreatedBy
	colCreatedAt
	colUpdatedAt
)

type projectSkillsPlugin struct {
	db  *plugin.DB
	log *plugin.Logger
}

func (p *projectSkillsPlugin) Init(ctx *plugin.Context) error {
	p.db = ctx.DB()
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
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type skillDetail struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
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
		Body        string   `json:"body"`
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
	body := strings.TrimSpace(payload.Body)
	if body == "" {
		res.Error(400, "body is required")
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
		`INSERT INTO project_skills (id, project_id, name, description, triggers, body, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.NewString(), projectID, name, description, string(triggersJSON), body,
		req.Caller.CallerID, now, now,
	)
	if err != nil {
		p.log.Error("createSkill insert failed: " + err.Error())
		res.Error(500, "failed to create skill")
		return
	}

	res.JSON(201, successEnvelope{Success: true, Data: skillDetail{
		Name:        name,
		Description: description,
		Triggers:    payload.Triggers,
		Content:     renderSkillMD(name, description, payload.Triggers, body),
	}})
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
	res.JSON(200, successEnvelope{Success: true, Data: skillDetail{
		Name:        toString(row, colName),
		Description: toString(row, colDescription),
		Triggers:    triggers,
		Content:     renderSkillMD(toString(row, colName), toString(row, colDescription), triggers, toString(row, colBody)),
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
		Body        string   `json:"body"`
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
	body := strings.TrimSpace(payload.Body)
	if body == "" {
		res.Error(400, "body is required")
		return
	}

	triggersJSON, _ := json.Marshal(payload.Triggers)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	rowsUpdated, err := p.db.Exec(
		`UPDATE project_skills SET description = $1, triggers = $2, body = $3, updated_at = $4
		 WHERE project_id = $5 AND name = $6`,
		description, string(triggersJSON), body, now, projectID, name,
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

	res.JSON(200, successEnvelope{Success: true, Data: skillDetail{
		Name:        name,
		Description: description,
		Triggers:    payload.Triggers,
		Content:     renderSkillMD(name, description, payload.Triggers, body),
	}})
}

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
