package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillContent(t *testing.T) {
	content := `---
name: test-skill
description: "A test skill for testing"
metadata: {"emoji":"🧪","requires":{"bins":["curl"]}}
---

# Test Skill

This is a test skill content.
`

	skill, err := ParseSkillContent(content, "/test/path/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillContent failed: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Expected name 'test-skill', got '%s'", skill.Name)
	}

	if skill.Description != "A test skill for testing" {
		t.Errorf("Expected description 'A test skill for testing', got '%s'", skill.Description)
	}

	if skill.Metadata.Emoji != "🧪" {
		t.Errorf("Expected emoji '🧪', got '%s'", skill.Metadata.Emoji)
	}

	if len(skill.Metadata.Requires.Bins) != 1 || skill.Metadata.Requires.Bins[0] != "curl" {
		t.Errorf("Expected bins ['curl'], got %v", skill.Metadata.Requires.Bins)
	}

	// Content should not have frontmatter
	if skill.Content == "" {
		t.Error("Expected content to be non-empty")
	}
}

func TestParseSkillContentWithoutFrontmatter(t *testing.T) {
	content := `# Simple Skill

This skill has no frontmatter.
`

	skill, err := ParseSkillContent(content, "/test/path/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillContent failed: %v", err)
	}

	if skill.Name != "" {
		t.Errorf("Expected empty name, got '%s'", skill.Name)
	}

	if skill.Content == "" {
		t.Error("Expected content to be non-empty")
	}
}

func TestListSkillDirs(t *testing.T) {
	// Create a temp directory structure
	tmpDir, err := os.MkdirTemp("", "skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create skill directories
	skill1Dir := filepath.Join(tmpDir, "skill1")
	skill2Dir := filepath.Join(tmpDir, "skill2")
	notASkillDir := filepath.Join(tmpDir, "notaskill")

	os.MkdirAll(skill1Dir, 0755)
	os.MkdirAll(skill2Dir, 0755)
	os.MkdirAll(notASkillDir, 0755)

	// Create SKILL.md files
	os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte("# Skill 1"), 0644)
	os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte("# Skill 2"), 0644)
	// NotASkillDir has no SKILL.md

	dirs, err := ListSkillDirs(tmpDir)
	if err != nil {
		t.Fatalf("ListSkillDirs failed: %v", err)
	}

	if len(dirs) != 2 {
		t.Errorf("Expected 2 skill dirs, got %d", len(dirs))
	}

	names := make(map[string]bool)
	for _, d := range dirs {
		names[d.Name] = true
	}

	if !names["skill1"] || !names["skill2"] {
		t.Errorf("Expected skill1 and skill2, got %v", names)
	}
}

func TestSkillAvailability(t *testing.T) {
	// Skill with no requirements should be available
	content := `---
name: available-skill
description: "A skill with no requirements"
---

# Available Skill
`
	skill, err := ParseSkillContent(content, "/test/path/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillContent failed: %v", err)
	}

	// On a typical system, curl won't be available in this test environment
	// so we can't reliably test availability without mocking
	_ = skill
}

func TestGetMissingDependencies(t *testing.T) {
	content := `---
name: deps-skill
description: "A skill with dependencies"
metadata: {"requires":{"bins":["nonexistent-binary-12345"],"env":["NONEXISTENT_ENV_VAR_12345"]}}
---

# Deps Skill
`
	skill, err := ParseSkillContent(content, "/test/path/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillContent failed: %v", err)
	}

	missing := skill.getMissingDependencies()
	if len(missing) != 2 {
		t.Errorf("Expected 2 missing deps, got %d: %v", len(missing), missing)
	}
}

func TestSkillLoader(t *testing.T) {
	// Create a temp directory structure
	tmpDir, err := os.MkdirTemp("", "skillloader-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	// Create a workspace skill
	workspaceSkillsDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(filepath.Join(workspaceSkillsDir, "ws-skill"), 0755)
	os.WriteFile(filepath.Join(workspaceSkillsDir, "ws-skill", "SKILL.md"), []byte(`---
name: ws-skill
description: "Workspace skill"
---

# Workspace Skill
`), 0644)

	loader := NewSkillLoader(workspaceSkillsDir, filepath.Join(tmpDir, "no-builtins"))

	// Load the skill
	skill := loader.LoadSkill("ws-skill")
	if skill == nil {
		t.Fatal("Failed to load workspace skill")
	}
	if skill.Source != "workspace" {
		t.Errorf("Expected source 'workspace', got '%s'", skill.Source)
	}

	// List skills
	skills := loader.ListSkills(false)
	if len(skills) != 1 {
		t.Errorf("Expected 1 skill, got %d", len(skills))
	}
}

func TestSkillLoaderDisabledSkillsAreHiddenFromModelFacingLists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-disabled-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	skillsDir := filepath.Join(tmpDir, "skills")
	writeSkillFile(t, filepath.Join(skillsDir, "demo", "SKILL.md"), `---
name: demo
description: "Demo skill"
---

# Demo Skill
`)

	loader := NewSkillLoader(skillsDir, filepath.Join(tmpDir, "no-builtins"))
	loader.SetDisabled([]string{"demo"})

	if got := loader.ListSkillNames(false); len(got) != 0 {
		t.Fatalf("disabled skill should be hidden from model-facing list, got %v", got)
	}
	all := loader.ListAllSkills(false)
	if len(all) != 1 || all[0].Name != "demo" || all[0].Enabled {
		t.Fatalf("expected disabled skill in management list, got %+v", all)
	}
	if expanded, ok := ExpandSkillCommand(loader, "/skill:demo inspect"); ok || expanded == "" {
		t.Fatalf("disabled skill should not expand, ok=%v expanded=%q", ok, expanded)
	}
}

func TestSkillLoaderDiscoversGenericAgentSkillDirsOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-discovery-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	home := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", home)

	writeSkillFile(t, filepath.Join(workspace, "skills", "ori-skill", "SKILL.md"), `---
name: ori-skill
description: "Ori workspace skill"
---

# Ori Skill
`)
	writeSkillFile(t, filepath.Join(workspace, ".agents", "skills", "team", "project-skill", "SKILL.md"), `---
name: project-skill
description: "Project agent skill"
---

# Project Skill
`)
	writeSkillFile(t, filepath.Join(home, ".agents", "skills", "user-skill", "SKILL.md"), `---
name: user-skill
description: "User agent skill"
---

# User Skill
`)
	writeSkillFile(t, filepath.Join(workspace, ".pi", "skills", "pi-skill", "SKILL.md"), `---
name: pi-skill
description: "Pi-specific skill"
---

# Pi Skill
`)

	loader := NewSkillLoader(filepath.Join(workspace, "skills"), filepath.Join(tmpDir, "no-builtins"))
	names := loader.ListSkillNames(false)

	for _, name := range []string{"ori-skill", "project-skill", "user-skill"} {
		if !containsString(names, name) {
			t.Fatalf("expected %s in discovered skills, got %v", name, names)
		}
	}
	if containsString(names, "pi-skill") {
		t.Fatalf("did not expect pi-specific skill directory to be discovered: %v", names)
	}
	if got := loader.LoadSkill("project-skill"); got == nil || got.Source != "project" {
		t.Fatalf("expected project skill source, got %+v", got)
	}
	if got := loader.LoadSkill("user-skill"); got == nil || got.Source != "user" {
		t.Fatalf("expected user skill source, got %+v", got)
	}
}

func TestSkillLoaderPrefersWorkspaceOverGenericAgentDirs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-priority-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	writeSkillFile(t, filepath.Join(workspace, "skills", "demo", "SKILL.md"), `---
name: demo
description: "Workspace copy"
---

# Workspace Demo
`)
	writeSkillFile(t, filepath.Join(workspace, ".agents", "skills", "demo", "SKILL.md"), `---
name: demo
description: "Project copy"
---

# Project Demo
`)

	loader := NewSkillLoader(filepath.Join(workspace, "skills"), filepath.Join(tmpDir, "no-builtins"))
	skill := loader.LoadSkill("demo")
	if skill == nil {
		t.Fatal("expected demo skill")
	}
	if skill.Source != "workspace" || skill.Description != "Workspace copy" {
		t.Fatalf("expected workspace skill to win, got source=%q description=%q", skill.Source, skill.Description)
	}
}

func TestParseSkillContentSupportsYAMLMetadata(t *testing.T) {
	content := `---
name: yaml-skill
description: "YAML skill"
metadata:
  always: true
  requires:
    env:
      - ORI_TEST_MISSING_ENV
allowed-tools:
  - read_file
  - shell
disable-model-invocation: true
---

# YAML Skill
`

	skill, err := ParseSkillContent(content, "/test/path/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillContent failed: %v", err)
	}
	if !skill.Metadata.Always {
		t.Fatalf("expected metadata.always to be parsed")
	}
	if len(skill.Metadata.Requires.Env) != 1 || skill.Metadata.Requires.Env[0] != "ORI_TEST_MISSING_ENV" {
		t.Fatalf("expected env requirement, got %+v", skill.Metadata.Requires.Env)
	}
	if !skill.DisableModelInvocation {
		t.Fatalf("expected disable-model-invocation to be parsed")
	}
	if got := strings.Join(skill.AllowedTools, ","); got != "read_file,shell" {
		t.Fatalf("expected allowed tools read_file,shell, got %q", got)
	}
}

func TestListSkillsSkipsMissingDescription(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-description-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillFile(t, filepath.Join(tmpDir, "skills", "missing-description", "SKILL.md"), `---
name: missing-description
---

# Missing Description
`)

	loader := NewSkillLoader(filepath.Join(tmpDir, "skills"), filepath.Join(tmpDir, "no-builtins"))
	names := loader.ListSkillNames(false)
	if containsString(names, "missing-description") {
		t.Fatalf("expected missing-description skill to be skipped, got %v", names)
	}
}

func TestBuildSkillsSummarySkipsModelDisabledSkills(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-summary-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeSkillFile(t, filepath.Join(tmpDir, "skills", "visible", "SKILL.md"), `---
name: visible
description: "Visible skill"
metadata:
  requires:
    env:
      - ORI_TEST_MISSING_ENV
---

# Visible
`)
	writeSkillFile(t, filepath.Join(tmpDir, "skills", "manual-only", "SKILL.md"), `---
name: manual-only
description: "Manual only skill"
disable-model-invocation: true
---

# Manual Only
`)

	loader := NewSkillLoader(filepath.Join(tmpDir, "skills"), filepath.Join(tmpDir, "no-builtins"))
	summary := loader.BuildSkillsSummary()
	if !strings.Contains(summary, "visible") || !strings.Contains(summary, "Visible skill") {
		t.Fatalf("expected visible skill in summary, got:\n%s", summary)
	}
	if !strings.Contains(summary, "ORI_TEST_MISSING_ENV") {
		t.Fatalf("expected missing dependency in summary, got:\n%s", summary)
	}
	if strings.Contains(summary, "manual-only") {
		t.Fatalf("did not expect disable-model-invocation skill in summary, got:\n%s", summary)
	}
}

func TestFormatSkillListIsStable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-list-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspaceSkillsDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(filepath.Join(workspaceSkillsDir, "zeta"), 0755)
	os.MkdirAll(filepath.Join(workspaceSkillsDir, "alpha"), 0755)
	os.WriteFile(filepath.Join(workspaceSkillsDir, "zeta", "SKILL.md"), []byte(`---
name: zeta
description: "Last skill"
---

# Zeta
`), 0644)
	os.WriteFile(filepath.Join(workspaceSkillsDir, "alpha", "SKILL.md"), []byte(`---
name: alpha
description: "First skill"
metadata: {"requires":{"bins":["nonexistent-skill-bin-12345"]}}
---

# Alpha
`), 0644)

	loader := NewSkillLoader(workspaceSkillsDir, "")
	out := FormatSkillList(loader.ListSkills(false))

	alphaIdx := strings.Index(out, "/skill:alpha")
	zetaIdx := strings.Index(out, "/skill:zeta")
	if alphaIdx < 0 || zetaIdx < 0 || alphaIdx > zetaIdx {
		t.Fatalf("expected stable sorted skill command list, got:\n%s", out)
	}
	if !strings.Contains(out, "workspace") || !strings.Contains(out, "missing: bin:nonexistent-skill-bin-12345") {
		t.Fatalf("expected source and missing deps in list, got:\n%s", out)
	}
}

func TestFormatSkillListIsReadableForLongDescriptions(t *testing.T) {
	longDescription := "Create distinctive, production-grade frontend interfaces with high design quality. " +
		"Use this skill when the user asks to build web components, pages, artifacts, posters, or applications. " +
		"Generates creative, polished code and UI design that avoids generic AI aesthetics."
	items := []*Skill{
		{Name: "frontend-design", Source: "user", Available: true, Description: longDescription},
		{Name: "weather", Source: "builtin", Available: true, Description: "Get current weather and forecasts."},
	}

	out := FormatSkillList(items)
	if !strings.Contains(out, "\n\n/skill:weather") {
		t.Fatalf("expected blank line between skills, got:\n%s", out)
	}
	if strings.Contains(out, "Generates creative") {
		t.Fatalf("expected long description to be truncated, got:\n%s", out)
	}
	if !strings.Contains(out, "...") {
		t.Fatalf("expected truncated description marker, got:\n%s", out)
	}
}

func TestExpandSkillCommandWrapsSkillContentAndArgs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-expand-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspaceSkillsDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(filepath.Join(workspaceSkillsDir, "demo"), 0755)
	os.WriteFile(filepath.Join(workspaceSkillsDir, "demo", "SKILL.md"), []byte(`---
name: demo
description: "Demo skill"
---

# Demo Skill

Use this carefully.
`), 0644)

	loader := NewSkillLoader(workspaceSkillsDir, "")
	expanded, ok := ExpandSkillCommand(loader, "/skill:demo inspect this")
	if !ok {
		t.Fatal("expected skill command to expand")
	}
	if strings.Contains(expanded, "description:") {
		t.Fatalf("expected frontmatter stripped, got:\n%s", expanded)
	}
	if !strings.Contains(expanded, `<skill name="demo"`) || !strings.Contains(expanded, "Use this carefully.") {
		t.Fatalf("expected skill block content, got:\n%s", expanded)
	}
	if !strings.HasSuffix(expanded, "User: inspect this") {
		t.Fatalf("expected args appended, got:\n%s", expanded)
	}
}

func writeSkillFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<test>", "&lt;test&gt;"},
		{"a & b", "a &amp; b"},
		{"no change", "no change"},
	}

	for _, tt := range tests {
		result := escapeXML(tt.input)
		if result != tt.expected {
			t.Errorf("escapeXML(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
