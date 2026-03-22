package skills

import (
	"embed"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed builtin/*
var builtinFS embed.FS

// SkillLoader loads skills from directories and provides skill access
type SkillLoader struct {
	workspaceDir    string
	builtinSkillsDir string
	loadedSkills    map[string]*Skill
}

// NewSkillLoader creates a new skill loader
func NewSkillLoader(workspaceDir string, builtinSkillsDir string) *SkillLoader {
	return &SkillLoader{
		workspaceDir:    workspaceDir,
		builtinSkillsDir: builtinSkillsDir,
		loadedSkills:    make(map[string]*Skill),
	}
}

// ListSkills returns all available skills with their metadata
func (l *SkillLoader) ListSkills(filterUnavailable bool) []*Skill {
	var result []*Skill

	// Workspace skills (highest priority)
	if l.workspaceDir != "" {
		workspaceSkills := l.listSkillsFromDir(l.workspaceDir, "workspace")
		for _, s := range workspaceSkills {
			result = append(result, s)
		}
	}

	// Built-in skills (lower priority, don't override workspace)
	if l.builtinSkillsDir != "" {
		builtinSkills := l.listSkillsFromDir(l.builtinSkillsDir, "builtin")
		for _, s := range builtinSkills {
			if _, exists := l.loadedSkills[s.Name]; !exists {
				result = append(result, s)
			}
		}
	}

	// Filter if requested
	if filterUnavailable {
		filtered := make([]*Skill, 0, len(result))
		for _, s := range result {
			if s.Available {
				filtered = append(filtered, s)
			}
		}
		return filtered
	}

	return result
}

// listSkillsFromDir loads all skills from a directory
func (l *SkillLoader) listSkillsFromDir(basePath string, source string) []*Skill {
	var result []*Skill

	dirs, err := ListSkillDirs(basePath)
	if err != nil {
		return result
	}

	for _, dir := range dirs {
		skill, err := ParseSkill(dir.Path)
		if err != nil {
			continue
		}
		skill.Source = source
		l.loadedSkills[skill.Name] = skill
		result = append(result, skill)
	}

	return result
}

// LoadSkill loads a specific skill by name
func (l *SkillLoader) LoadSkill(name string) *Skill {
	// Check cache first
	if skill, exists := l.loadedSkills[name]; exists {
		return skill
	}

	// Try workspace first
	if l.workspaceDir != "" {
		skillPath := filepath.Join(l.workspaceDir, name, "SKILL.md")
		if skill, err := ParseSkill(skillPath); err == nil {
			skill.Source = "workspace"
			l.loadedSkills[name] = skill
			return skill
		}
	}

	// Try built-in
	if l.builtinSkillsDir != "" {
		skillPath := filepath.Join(l.builtinSkillsDir, name, "SKILL.md")
		if skill, err := ParseSkill(skillPath); err == nil {
			skill.Source = "builtin"
			l.loadedSkills[name] = skill
			return skill
		}
	}

	return nil
}

// LoadSkillsForContext loads specific skills and returns formatted content
func (l *SkillLoader) LoadSkillsForContext(names []string) string {
	var parts []string

	for _, name := range names {
		skill := l.LoadSkill(name)
		if skill == nil {
			continue
		}
		parts = append(parts, skill.Content)
	}

	return joinSkillContent(parts)
}

// BuildSkillsSummary returns an XML-formatted summary of all skills
func (l *SkillLoader) BuildSkillsSummary() string {
	allSkills := l.ListSkills(false)
	if len(allSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<skills>")

	for _, s := range allSkills {
		available := "true"
		if !s.Available {
			available = "false"
		}

		lines = append(lines, "  <skill available=\""+available+"\">")
		lines = append(lines, "    <name>"+escapeXML(s.Name)+"</name>")
		lines = append(lines, "    <description>"+escapeXML(s.Description)+"</description>")
		lines = append(lines, "    <location>"+escapeXML(s.Path)+"</location>")

		if !s.Available && len(s.MissingDeps) > 0 {
			lines = append(lines, "    <requires>"+escapeXML(joinStrings(s.MissingDeps, ", "))+"</requires>")
		}

		lines = append(lines, "  </skill>")
	}

	lines = append(lines, "</skills>")
	return joinStrings(lines, "\n")
}

// GetAlwaysSkills returns skills marked as always=true that are available
func (l *SkillLoader) GetAlwaysSkills() []string {
	var result []string

	for _, s := range l.ListSkills(true) {
		if s.Always || s.Metadata.Always {
			result = append(result, s.Name)
		}
	}

	return result
}

// GetSkillMetadata returns metadata for a specific skill
func (l *SkillLoader) GetSkillMetadata(name string) *SkillMetadata {
	skill := l.LoadSkill(name)
	if skill == nil {
		return nil
	}
	return &skill.Metadata
}

// ListSkillNames returns all skill names (available only)
func (l *SkillLoader) ListSkillNames(availableOnly bool) []string {
	var names []string
	skills := l.ListSkills(availableOnly)
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return names
}

// escapeXML escapes special XML characters
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}
	return result
}

func joinSkillContent(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "\n\n---\n\n" + parts[i]
	}
	return result
}

// GetBuiltinSkillFS returns the embedded builtin filesystem
func GetBuiltinSkillFS() embed.FS {
	return builtinFS
}

// GetRuntimeGOOS returns the runtime GOOS
func GetRuntimeGOOS() string {
	return runtime.GOOS
}
