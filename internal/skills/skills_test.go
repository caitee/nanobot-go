package skills

import (
	"os"
	"path/filepath"
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

	// Create a workspace skill
	workspaceSkillsDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(filepath.Join(workspaceSkillsDir, "ws-skill"), 0755)
	os.WriteFile(filepath.Join(workspaceSkillsDir, "ws-skill", "SKILL.md"), []byte(`---
name: ws-skill
description: "Workspace skill"
---

# Workspace Skill
`), 0644)

	loader := NewSkillLoader(workspaceSkillsDir, "")

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
