package main

import (
	"encoding/json"
	"testing"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Paca-AI/plugin-sdk-go/plugintest"
)

const testProjectID = "project-1"
const testDocID = "doc-1"

// testDocContent is a BlockNote block array (as the host's real
// `documents.content` jsonb column stores it) whose markdown rendering is
// exactly "1. Run tests\n2. Tag release" — see TestGetSkillRendersValidSkillMD.
const testDocContent = `[
	{"type":"numberedListItem","content":[{"type":"text","text":"Run tests","styles":{}}]},
	{"type":"numberedListItem","content":[{"type":"text","text":"Tag release","styles":{}}]}
]`

func setupPlugin(t *testing.T) *plugintest.Context {
	t.Helper()
	tc := plugintest.NewContext(t)
	tc.DB.SeedRows(
		"project_skills",
		[]string{"id", "project_id", "name", "description", "triggers", "created_by", "created_at", "updated_at", "doc_id"},
		nil,
	)
	tc.DB.SeedRows(
		"documents",
		[]string{"id", "project_id", "content", "deleted_at"},
		[][]any{
			{testDocID, testProjectID, testDocContent, nil},
		},
	)
	tc.DB.SeedRows(
		"project_skill_files",
		[]string{"id", "skill_id", "path", "content", "created_at", "updated_at"},
		nil,
	)

	var p projectSkillsPlugin
	if err := p.Init(tc.PluginContext()); err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}
	return tc
}

func req() plugintest.Request {
	return plugintest.Request{
		Caller: plugin.CallerIdentity{
			ProjectID:  testProjectID,
			CallerID:   "member-1",
			CallerRole: "PROJECT_MEMBER",
		},
	}
}

func TestCreateAppliesPacaPrefixAndListsSkill(t *testing.T) {
	tc := setupPlugin(t)

	create := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "new-docker-service",
		"description": "Scaffold a new Dockerized service for this project.",
		"doc_id":      testDocID,
	}))
	if create.StatusCode != 201 {
		t.Fatalf("expected 201, got %d: %s", create.StatusCode, create.BodyString())
	}

	var createEnv struct {
		Data skillDetail `json:"data"`
	}
	if err := json.Unmarshal(create.Body, &createEnv); err != nil {
		t.Fatal(err)
	}
	if createEnv.Data.Name != "paca-new-docker-service" {
		t.Fatalf("expected name to be prefixed with paca-, got %q", createEnv.Data.Name)
	}
	if createEnv.Data.DocID != testDocID {
		t.Fatalf("expected doc_id %q, got %q", testDocID, createEnv.Data.DocID)
	}

	list := tc.Call("GET", "/projects/:projectId/skills", req())
	if list.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", list.StatusCode, list.BodyString())
	}
	var listEnv struct {
		Data []skillSummary `json:"data"`
	}
	_ = json.Unmarshal(list.Body, &listEnv)
	if len(listEnv.Data) != 1 || listEnv.Data[0].Name != "paca-new-docker-service" {
		t.Fatalf("unexpected list response: %+v", listEnv)
	}
}

func TestCreateRejectsReservedTriggerPrefix(t *testing.T) {
	tc := setupPlugin(t)

	create := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "trigger-chat",
		"description": "Should be rejected.",
		"doc_id":      testDocID,
	}))
	if create.StatusCode != 400 {
		t.Fatalf("expected 400 for reserved prefix, got %d: %s", create.StatusCode, create.BodyString())
	}
}

func TestCreateRejectsInvalidSlug(t *testing.T) {
	tc := setupPlugin(t)

	create := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "Not A Slug!",
		"description": "Should be rejected.",
		"doc_id":      testDocID,
	}))
	if create.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid slug, got %d: %s", create.StatusCode, create.BodyString())
	}
}

func TestCreateRejectsUnknownDocID(t *testing.T) {
	tc := setupPlugin(t)

	create := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "docker-service",
		"description": "Should be rejected.",
		"doc_id":      "does-not-exist",
	}))
	if create.StatusCode != 400 {
		t.Fatalf("expected 400 for unknown doc_id, got %d: %s", create.StatusCode, create.BodyString())
	}
}

func TestCreateDuplicateNameConflicts(t *testing.T) {
	tc := setupPlugin(t)

	body := map[string]any{
		"name":        "docs-review",
		"description": "Review docs.",
		"doc_id":      testDocID,
	}
	first := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(body))
	if first.StatusCode != 201 {
		t.Fatalf("expected first create to succeed, got %d: %s", first.StatusCode, first.BodyString())
	}

	second := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(body))
	if second.StatusCode != 409 {
		t.Fatalf("expected 409 for duplicate name, got %d: %s", second.StatusCode, second.BodyString())
	}
}

func TestGetSkillRendersValidSkillMD(t *testing.T) {
	tc := setupPlugin(t)

	_ = tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "release-checklist",
		"description": "Walk through this project's release checklist.",
		"triggers":    []string{"/release"},
		"doc_id":      testDocID,
	}))

	get := tc.Call("GET", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-release-checklist"},
	})
	if get.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", get.StatusCode, get.BodyString())
	}

	var env struct {
		Data skillDetail `json:"data"`
	}
	_ = json.Unmarshal(get.Body, &env)

	wantContent := "---\n" +
		`name: "paca-release-checklist"` + "\n" +
		`description: "Walk through this project's release checklist."` + "\n" +
		"triggers:\n" +
		`  - "/release"` + "\n" +
		"---\n\n" +
		"1. Run tests\n2. Tag release\n"
	if env.Data.Content != wantContent {
		t.Fatalf("unexpected SKILL.md content:\n%s\n---want---\n%s", env.Data.Content, wantContent)
	}
}

func TestUpdateAndDeleteSkill(t *testing.T) {
	tc := setupPlugin(t)

	_ = tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        "onboarding",
		"description": "Original description.",
		"doc_id":      testDocID,
	}))

	update := tc.Call("PATCH", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	}.WithJSONBody(map[string]any{
		"description": "Updated description.",
	}))
	if update.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", update.StatusCode, update.BodyString())
	}
	var updateEnv struct {
		Data skillDetail `json:"data"`
	}
	_ = json.Unmarshal(update.Body, &updateEnv)
	if updateEnv.Data.DocID != testDocID {
		t.Fatalf("expected doc_id to remain %q after update, got %q", testDocID, updateEnv.Data.DocID)
	}

	remove := tc.Call("DELETE", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	})
	if remove.StatusCode != 204 {
		t.Fatalf("expected 204, got %d: %s", remove.StatusCode, remove.BodyString())
	}

	getAfterDelete := tc.Call("GET", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	})
	if getAfterDelete.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", getAfterDelete.StatusCode)
	}
}

func TestDeleteMissingSkillReturns404(t *testing.T) {
	tc := setupPlugin(t)

	remove := tc.Call("DELETE", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-does-not-exist"},
	})
	if remove.StatusCode != 404 {
		t.Fatalf("expected 404, got %d: %s", remove.StatusCode, remove.BodyString())
	}
}

func TestMissingProjectScopeReturns400(t *testing.T) {
	tc := setupPlugin(t)
	noScope := plugintest.Request{Caller: plugin.CallerIdentity{CallerID: "member-1"}}

	cases := []struct {
		method string
		path   string
		req    plugintest.Request
	}{
		{"GET", "/projects/:projectId/skills", noScope},
		{"POST", "/projects/:projectId/skills", noScope.WithJSONBody(map[string]any{"name": "x", "description": "d", "doc_id": testDocID})},
		{"GET", "/projects/:projectId/skills/:name", plugintest.Request{Caller: noScope.Caller, PathParams: map[string]string{"name": "paca-x"}}},
		{"PATCH", "/projects/:projectId/skills/:name", plugintest.Request{Caller: noScope.Caller, PathParams: map[string]string{"name": "paca-x"}}.WithJSONBody(map[string]any{"description": "d"})},
		{"DELETE", "/projects/:projectId/skills/:name", plugintest.Request{Caller: noScope.Caller, PathParams: map[string]string{"name": "paca-x"}}},
	}
	for _, c := range cases {
		res := tc.Call(c.method, c.path, c.req)
		if res.StatusCode != 400 {
			t.Errorf("%s %s: expected 400 for missing project scope, got %d: %s", c.method, c.path, res.StatusCode, res.BodyString())
		}
	}
}

func TestCreateValidationErrors(t *testing.T) {
	tc := setupPlugin(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing name", map[string]any{"description": "d", "doc_id": testDocID}},
		{"missing description", map[string]any{"name": "valid-name", "doc_id": testDocID}},
		{"blank description", map[string]any{"name": "valid-name", "description": "   ", "doc_id": testDocID}},
		{"missing doc_id", map[string]any{"name": "valid-name", "description": "d"}},
		{"blank doc_id", map[string]any{"name": "valid-name", "description": "d", "doc_id": "   "}},
	}
	for _, c := range cases {
		res := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(c.body))
		if res.StatusCode != 400 {
			t.Errorf("%s: expected 400, got %d: %s", c.name, res.StatusCode, res.BodyString())
		}
	}
}

func TestCreateInvalidJSONBodyReturns400(t *testing.T) {
	tc := setupPlugin(t)

	res := tc.Call("POST", "/projects/:projectId/skills", plugintest.Request{
		Caller: req().Caller,
		Body:   []byte("{not json"),
	})
	if res.StatusCode != 400 {
		t.Fatalf("expected 400, got %d: %s", res.StatusCode, res.BodyString())
	}
}

func TestUpdateValidationErrors(t *testing.T) {
	tc := setupPlugin(t)
	_ = tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name": "editable", "description": "d", "doc_id": testDocID,
	}))

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing description", map[string]any{}},
		{"blank description", map[string]any{"description": "  "}},
	}
	for _, c := range cases {
		res := tc.Call("PATCH", "/projects/:projectId/skills/:name", plugintest.Request{
			Caller:     req().Caller,
			PathParams: map[string]string{"name": "paca-editable"},
		}.WithJSONBody(c.body))
		if res.StatusCode != 400 {
			t.Errorf("%s: expected 400, got %d: %s", c.name, res.StatusCode, res.BodyString())
		}
	}
}

func TestUpdateInvalidJSONBodyReturns400(t *testing.T) {
	tc := setupPlugin(t)

	res := tc.Call("PATCH", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-whatever"},
		Body:       []byte("{not json"),
	})
	if res.StatusCode != 400 {
		t.Fatalf("expected 400, got %d: %s", res.StatusCode, res.BodyString())
	}
}

func TestSkillsFolderRoundTrip(t *testing.T) {
	tc := setupPlugin(t)

	empty := tc.Call("GET", "/projects/:projectId/skills-folder", req())
	if empty.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", empty.StatusCode, empty.BodyString())
	}
	var emptyEnv struct {
		Data struct {
			DocFolderID string `json:"doc_folder_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(empty.Body, &emptyEnv)
	if emptyEnv.Data.DocFolderID != "" {
		t.Fatalf("expected empty doc_folder_id before it's ever set, got %q", emptyEnv.Data.DocFolderID)
	}

	set := tc.Call("POST", "/projects/:projectId/skills-folder", req().WithJSONBody(map[string]any{
		"doc_folder_id": "folder-1",
	}))
	if set.StatusCode != 204 {
		t.Fatalf("expected 204, got %d: %s", set.StatusCode, set.BodyString())
	}

	get := tc.Call("GET", "/projects/:projectId/skills-folder", req())
	var getEnv struct {
		Data struct {
			DocFolderID string `json:"doc_folder_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(get.Body, &getEnv)
	if getEnv.Data.DocFolderID != "folder-1" {
		t.Fatalf("expected doc_folder_id %q, got %q", "folder-1", getEnv.Data.DocFolderID)
	}
}

func TestSkillsFolderValidation(t *testing.T) {
	tc := setupPlugin(t)

	missingScope := tc.Call("GET", "/projects/:projectId/skills-folder", plugintest.Request{
		Caller: plugin.CallerIdentity{CallerID: "member-1"},
	})
	if missingScope.StatusCode != 400 {
		t.Errorf("GET missing scope: expected 400, got %d", missingScope.StatusCode)
	}

	blank := tc.Call("POST", "/projects/:projectId/skills-folder", req().WithJSONBody(map[string]any{
		"doc_folder_id": "  ",
	}))
	if blank.StatusCode != 400 {
		t.Errorf("PUT blank doc_folder_id: expected 400, got %d", blank.StatusCode)
	}

	invalidJSON := tc.Call("POST", "/projects/:projectId/skills-folder", plugintest.Request{
		Caller: req().Caller,
		Body:   []byte("{not json"),
	})
	if invalidJSON.StatusCode != 400 {
		t.Errorf("PUT invalid JSON: expected 400, got %d", invalidJSON.StatusCode)
	}
}

func createTestSkill(t *testing.T, tc *plugintest.Context, name string) {
	t.Helper()
	res := tc.Call("POST", "/projects/:projectId/skills", req().WithJSONBody(map[string]any{
		"name":        name,
		"description": "d",
		"doc_id":      testDocID,
	}))
	if res.StatusCode != 201 {
		t.Fatalf("createTestSkill(%q): expected 201, got %d: %s", name, res.StatusCode, res.BodyString())
	}
}

func TestSkillFileUpsertCreateAndUpdate(t *testing.T) {
	tc := setupPlugin(t)
	createTestSkill(t, tc, "with-files")

	create := tc.Call("POST", "/projects/:projectId/skills/:name/files", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-with-files"},
	}.WithJSONBody(map[string]any{
		"path":    "references/notes.md",
		"content": "first version",
	}))
	if create.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", create.StatusCode, create.BodyString())
	}

	get := tc.Call("GET", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-with-files"},
	})
	var getEnv struct {
		Data skillDetail `json:"data"`
	}
	_ = json.Unmarshal(get.Body, &getEnv)
	if len(getEnv.Data.Files) != 1 || getEnv.Data.Files[0].Content != "first version" {
		t.Fatalf("expected one file with content %q, got %+v", "first version", getEnv.Data.Files)
	}

	update := tc.Call("POST", "/projects/:projectId/skills/:name/files", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-with-files"},
	}.WithJSONBody(map[string]any{
		"path":    "references/notes.md",
		"content": "second version",
	}))
	if update.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", update.StatusCode, update.BodyString())
	}

	getAfterUpdate := tc.Call("GET", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-with-files"},
	})
	var getAfterEnv struct {
		Data skillDetail `json:"data"`
	}
	_ = json.Unmarshal(getAfterUpdate.Body, &getAfterEnv)
	if len(getAfterEnv.Data.Files) != 1 || getAfterEnv.Data.Files[0].Content != "second version" {
		t.Fatalf("expected the same path updated in place with content %q, got %+v", "second version", getAfterEnv.Data.Files)
	}
}

func TestSkillFileUpsertRejectsInvalidPaths(t *testing.T) {
	tc := setupPlugin(t)
	createTestSkill(t, tc, "bad-paths")

	cases := []struct {
		name string
		path string
	}{
		{"path traversal", "references/../../etc/passwd"},
		{"absolute path", "/references/notes.md"},
		{"wrong top-level dir", "assets/image.png"},
		{"no top-level dir", "notes.md"},
		{"nested subdirectory", "references/sub/notes.md"},
		{"empty path", ""},
	}
	for _, c := range cases {
		res := tc.Call("POST", "/projects/:projectId/skills/:name/files", plugintest.Request{
			Caller:     req().Caller,
			PathParams: map[string]string{"name": "paca-bad-paths"},
		}.WithJSONBody(map[string]any{
			"path":    c.path,
			"content": "x",
		}))
		if res.StatusCode != 400 {
			t.Errorf("%s (%q): expected 400, got %d: %s", c.name, c.path, res.StatusCode, res.BodyString())
		}
	}
}

func TestSkillFileUpsertMissingSkillReturns404(t *testing.T) {
	tc := setupPlugin(t)

	res := tc.Call("POST", "/projects/:projectId/skills/:name/files", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-does-not-exist"},
	}.WithJSONBody(map[string]any{
		"path":    "scripts/setup.sh",
		"content": "#!/bin/sh",
	}))
	if res.StatusCode != 404 {
		t.Fatalf("expected 404, got %d: %s", res.StatusCode, res.BodyString())
	}
}

func TestSkillFileDelete(t *testing.T) {
	tc := setupPlugin(t)
	createTestSkill(t, tc, "delete-me")

	_ = tc.Call("POST", "/projects/:projectId/skills/:name/files", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-delete-me"},
	}.WithJSONBody(map[string]any{
		"path":    "scripts/setup.sh",
		"content": "#!/bin/sh\necho hi",
	}))

	del := tc.Call("POST", "/projects/:projectId/skills/:name/files/delete", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-delete-me"},
	}.WithJSONBody(map[string]any{"path": "scripts/setup.sh"}))
	if del.StatusCode != 204 {
		t.Fatalf("expected 204, got %d: %s", del.StatusCode, del.BodyString())
	}

	get := tc.Call("GET", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-delete-me"},
	})
	var getEnv struct {
		Data skillDetail `json:"data"`
	}
	_ = json.Unmarshal(get.Body, &getEnv)
	if len(getEnv.Data.Files) != 0 {
		t.Fatalf("expected no files after delete, got %+v", getEnv.Data.Files)
	}

	deleteAgain := tc.Call("POST", "/projects/:projectId/skills/:name/files/delete", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-delete-me"},
	}.WithJSONBody(map[string]any{"path": "scripts/setup.sh"}))
	if deleteAgain.StatusCode != 404 {
		t.Fatalf("expected 404 deleting an already-deleted file, got %d: %s", deleteAgain.StatusCode, deleteAgain.BodyString())
	}
}

func TestShutdownDoesNotPanic(t *testing.T) {
	tc := plugintest.NewContext(t)
	tc.DB.SeedRows(
		"project_skills",
		[]string{"id", "project_id", "name", "description", "triggers", "created_by", "created_at", "updated_at", "doc_id"},
		nil,
	)
	var p projectSkillsPlugin
	if err := p.Init(tc.PluginContext()); err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}
	p.Shutdown()
}

func TestDecodeTriggersMalformedJSONReturnsNil(t *testing.T) {
	if got := decodeTriggers("not valid json"); got != nil {
		t.Fatalf("expected nil for malformed triggers JSON, got %v", got)
	}
	if got := decodeTriggers(""); got != nil {
		t.Fatalf("expected nil for empty triggers, got %v", got)
	}
}

func TestYamlQuoteEscapesControlCharacters(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", `"hello"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"double quote", `say "hi"`, `"say \"hi\""`},
		{"newline", "line one\nline two", `"line one\nline two"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
	}
	for _, c := range cases {
		got := yamlQuote(c.in)
		if got != c.want {
			t.Errorf("%s: yamlQuote(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestNormalizeNameEdgeCases(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"paca-already-prefixed", "paca-already-prefixed", false},
		{"Mixed-CASE-Name", "paca-mixed-case-name", false},
		{"", "", true},
		{"   ", "", true},
		{"Not A Slug!", "", true},
		{"double--hyphen", "", true},
		{"trigger-anything", "", true},
		{"paca-trigger-anything", "", true},
	}
	for _, c := range cases {
		got, err := normalizeName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeName(%q): expected error, got name %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeName(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBlocksToMarkdown(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"empty", "", ""},
		{"null", "null", ""},
		{
			"heading and paragraph",
			`[{"type":"heading","props":{"level":2},"content":[{"type":"text","text":"Steps","styles":{}}]},
			  {"type":"paragraph","content":[{"type":"text","text":"Do the thing.","styles":{}}]}]`,
			"## Steps\n\nDo the thing.",
		},
		{
			"bold and code marks",
			`[{"type":"paragraph","content":[
				{"type":"text","text":"Run ","styles":{}},
				{"type":"text","text":"go test","styles":{"code":true}},
				{"type":"text","text":" first","styles":{"bold":true}}
			]}]`,
			"Run `go test`** first**",
		},
		{
			"bullet list",
			`[{"type":"bulletListItem","content":[{"type":"text","text":"a","styles":{}}]},
			  {"type":"bulletListItem","content":[{"type":"text","text":"b","styles":{}}]}]`,
			"- a\n- b",
		},
		{"malformed json", "{not json", ""},
		{
			"heading level clamped below 1",
			`[{"type":"heading","props":{"level":0},"content":[{"type":"text","text":"x","styles":{}}]}]`,
			"# x",
		},
		{
			"heading level clamped above 6",
			`[{"type":"heading","props":{"level":99},"content":[{"type":"text","text":"x","styles":{}}]}]`,
			"###### x",
		},
		{
			"heading with no level defaults to 1",
			`[{"type":"heading","props":{},"content":[{"type":"text","text":"x","styles":{}}]}]`,
			"# x",
		},
		{
			"checklist unchecked and checked",
			`[{"type":"checkListItem","props":{"checked":false},"content":[{"type":"text","text":"todo","styles":{}}]},
			  {"type":"checkListItem","props":{"checked":true},"content":[{"type":"text","text":"done","styles":{}}]}]`,
			"- [ ] todo\n- [x] done",
		},
		{
			"code block with language",
			`[{"type":"codeBlock","props":{"language":"go"},"content":[{"type":"text","text":"fmt.Println(1)","styles":{}}]}]`,
			"```go\nfmt.Println(1)\n```",
		},
		{
			"code block with no language",
			`[{"type":"codeBlock","props":{},"content":[{"type":"text","text":"echo hi","styles":{}}]}]`,
			"```\necho hi\n```",
		},
		{
			"quote",
			`[{"type":"quote","content":[{"type":"text","text":"be excellent","styles":{}}]}]`,
			"> be excellent",
		},
		{
			"numbered list resets across an interruption",
			`[{"type":"numberedListItem","content":[{"type":"text","text":"one","styles":{}}]},
			  {"type":"numberedListItem","content":[{"type":"text","text":"two","styles":{}}]},
			  {"type":"paragraph","content":[{"type":"text","text":"interrupt","styles":{}}]},
			  {"type":"numberedListItem","content":[{"type":"text","text":"restarts at one","styles":{}}]}]`,
			"1. one\n2. two\ninterrupt\n\n1. restarts at one",
		},
		{
			"nested bullet list under a parent item",
			`[{"type":"bulletListItem","content":[{"type":"text","text":"parent","styles":{}}],
			   "children":[{"type":"bulletListItem","content":[{"type":"text","text":"child","styles":{}}]}]}]`,
			"- parent\n  - child",
		},
		{
			"link",
			`[{"type":"paragraph","content":[{"type":"link","href":"https://example.com","content":[{"type":"text","text":"click","styles":{}}]}]}]`,
			"[click](https://example.com)",
		},
		{
			"italic and strike",
			`[{"type":"paragraph","content":[
				{"type":"text","text":"a","styles":{"italic":true}},
				{"type":"text","text":"b","styles":{"strike":true}}
			]}]`,
			"_a_~~b~~",
		},
		{
			"unknown block type falls back to its text content",
			`[{"type":"someFutureBlockType","content":[{"type":"text","text":"still readable","styles":{}}]}]`,
			"still readable",
		},
		{
			"unknown block type with empty content is omitted",
			`[{"type":"someFutureBlockType","content":[]}]`,
			"",
		},
		{
			"empty paragraph produces a blank line, trimmed at the edges",
			`[{"type":"paragraph","content":[{"type":"text","text":"before","styles":{}}]},
			  {"type":"paragraph","content":[]},
			  {"type":"paragraph","content":[{"type":"text","text":"after","styles":{}}]}]`,
			"before\n\n\nafter",
		},
	}
	for _, c := range cases {
		got := blocksToMarkdown([]byte(c.json))
		if got != c.want {
			t.Errorf("%s: blocksToMarkdown = %q, want %q", c.name, got, c.want)
		}
	}
}
