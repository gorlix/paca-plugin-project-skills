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

	create := tc.Call("POST", "/skills", req().WithJSONBody(map[string]any{
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

	list := tc.Call("GET", "/skills", req())
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

	create := tc.Call("POST", "/skills", req().WithJSONBody(map[string]any{
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

	create := tc.Call("POST", "/skills", req().WithJSONBody(map[string]any{
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
	first := tc.Call("POST", "/skills", req().WithJSONBody(body))
	if first.StatusCode != 201 {
		t.Fatalf("expected first create to succeed, got %d: %s", first.StatusCode, first.BodyString())
	}

	second := tc.Call("POST", "/skills", req().WithJSONBody(body))
	if second.StatusCode != 409 {
		t.Fatalf("expected 409 for duplicate name, got %d: %s", second.StatusCode, second.BodyString())
	}
}

func TestGetSkillRendersValidSkillMD(t *testing.T) {
	tc := setupPlugin(t)

	_ = tc.Call("POST", "/skills", req().WithJSONBody(map[string]any{
		"name":        "release-checklist",
		"description": "Walk through this project's release checklist.",
		"triggers":    []string{"/release"},
		"body":        "1. Run tests\n2. Tag release",
	}))

	get := tc.Call("GET", "/skills/:name", plugintest.Request{
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

	_ = tc.Call("POST", "/skills", req().WithJSONBody(map[string]any{
		"name":        "onboarding",
		"description": "Original description.",
		"body":        "Original body.",
	}))

	update := tc.Call("PATCH", "/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	}.WithJSONBody(map[string]any{
		"description": "Updated description.",
		"body":        "Updated body.",
	}))
	if update.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", update.StatusCode, update.BodyString())
	}

	remove := tc.Call("DELETE", "/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	})
	if remove.StatusCode != 204 {
		t.Fatalf("expected 204, got %d: %s", remove.StatusCode, remove.BodyString())
	}

	getAfterDelete := tc.Call("GET", "/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-onboarding"},
	})
	if getAfterDelete.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", getAfterDelete.StatusCode)
	}
}

func TestDeleteMissingSkillReturns404(t *testing.T) {
	tc := setupPlugin(t)

	remove := tc.Call("DELETE", "/skills/:name", plugintest.Request{
		Caller:     req().Caller,
		PathParams: map[string]string{"name": "paca-does-not-exist"},
	})
	if remove.StatusCode != 404 {
		t.Fatalf("expected 404, got %d: %s", remove.StatusCode, remove.BodyString())
	}
}
