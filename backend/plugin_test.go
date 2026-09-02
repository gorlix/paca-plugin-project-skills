package main

import (
	"encoding/json"
	"testing"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Paca-AI/plugin-sdk-go/plugintest"
)

const testProjectID = "project-1"

func setupPlugin(t *testing.T) *plugintest.Context {
	t.Helper()
	tc := plugintest.NewContext(t)
	tc.DB.SeedRows(
		"project_skills",
		[]string{"id", "project_id", "name", "description", "triggers", "body", "created_by", "created_at", "updated_at"},
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
		"body":        "Follow the project's service template exactly.",
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
		"body":        "n/a",
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
		"body":        "n/a",
	}))
	if create.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid slug, got %d: %s", create.StatusCode, create.BodyString())
	}
}

func TestCreateDuplicateNameConflicts(t *testing.T) {
	tc := setupPlugin(t)

	body := map[string]any{
		"name":        "docs-review",
		"description": "Review docs.",
		"body":        "n/a",
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
		"body":        "1. Run tests\n2. Tag release",
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
		"body":        "Original body.",
	}))

	update := tc.Call("PATCH", "/projects/:projectId/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	}.WithJSONBody(map[string]any{
		"description": "Updated description.",
		"body":        "Updated body.",
	}))
	if update.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", update.StatusCode, update.BodyString())
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
		{"POST", "/projects/:projectId/skills", noScope.WithJSONBody(map[string]any{"name": "x", "description": "d", "body": "b"})},
		{"GET", "/projects/:projectId/skills/:name", plugintest.Request{Caller: noScope.Caller, PathParams: map[string]string{"name": "paca-x"}}},
		{"PATCH", "/projects/:projectId/skills/:name", plugintest.Request{Caller: noScope.Caller, PathParams: map[string]string{"name": "paca-x"}}.WithJSONBody(map[string]any{"description": "d", "body": "b"})},
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
		{"missing name", map[string]any{"description": "d", "body": "b"}},
		{"missing description", map[string]any{"name": "valid-name", "body": "b"}},
		{"blank description", map[string]any{"name": "valid-name", "description": "   ", "body": "b"}},
		{"missing body", map[string]any{"name": "valid-name", "description": "d"}},
		{"blank body", map[string]any{"name": "valid-name", "description": "d", "body": "   "}},
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
		"name": "editable", "description": "d", "body": "b",
	}))

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing description", map[string]any{"body": "b"}},
		{"blank description", map[string]any{"description": "  ", "body": "b"}},
		{"missing body", map[string]any{"description": "d"}},
		{"blank body", map[string]any{"description": "d", "body": "  "}},
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

func TestShutdownDoesNotPanic(t *testing.T) {
	tc := plugintest.NewContext(t)
	tc.DB.SeedRows(
		"project_skills",
		[]string{"id", "project_id", "name", "description", "triggers", "body", "created_by", "created_at", "updated_at"},
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
