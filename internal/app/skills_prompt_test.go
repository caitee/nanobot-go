package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ori/internal/skills"
)

func TestBuildSystemPromptIncludesSkillSummaryAndAlwaysSkills(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ori-skill-prompt-test")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	writeAppSkillFile(t, filepath.Join(workspace, "skills", "regular", "SKILL.md"), `---
name: regular
description: "Regular skill"
---

# Regular Skill

Regular body should stay out of the prompt summary.
`)
	writeAppSkillFile(t, filepath.Join(workspace, "skills", "always-on", "SKILL.md"), `---
name: always-on
description: "Always-on skill"
always: true
---

# Always-On Skill

Always body should be injected.
`)

	loader := skills.NewSkillLoader(filepath.Join(workspace, "skills"), filepath.Join(tmpDir, "no-builtins"))
	prompt := buildSystemPrompt(workspace, loader)

	if !strings.Contains(prompt, "<skills>") || !strings.Contains(prompt, "Regular skill") {
		t.Fatalf("expected skill summary in system prompt, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "Regular body should stay out") {
		t.Fatalf("did not expect regular skill body in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "# Always-On Skill") || !strings.Contains(prompt, "Always body should be injected.") {
		t.Fatalf("expected always skill body in prompt, got:\n%s", prompt)
	}
}

func writeAppSkillFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}
