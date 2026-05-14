package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

//go:embed builtin/*
var builtinFS embed.FS

// SkillLoader loads skills from directories and provides skill access
type SkillLoader struct {
	workspaceDir     string
	builtinSkillsDir string
	loadedSkills     map[string]*Skill
	mu               sync.RWMutex
	disabled         map[string]bool
}

type skillSource struct {
	path   string
	source string
}

// NewSkillLoader creates a new skill loader
func NewSkillLoader(workspaceDir string, builtinSkillsDir string) *SkillLoader {
	return &SkillLoader{
		workspaceDir:     workspaceDir,
		builtinSkillsDir: builtinSkillsDir,
		loadedSkills:     make(map[string]*Skill),
		disabled:         map[string]bool{},
	}
}

// ListSkills returns all available skills with their metadata
func (l *SkillLoader) ListSkills(filterUnavailable bool) []*Skill {
	all := l.ListAllSkills(filterUnavailable)
	result := make([]*Skill, 0, len(all))
	for _, skill := range all {
		if skill.Enabled {
			result = append(result, skill)
		}
	}
	return result
}

// ListAllSkills returns all discoverable skills, including disabled skills.
func (l *SkillLoader) ListAllSkills(filterUnavailable bool) []*Skill {
	var result []*Skill
	seen := map[string]bool{}

	addSkills := func(items []*Skill) {
		for _, s := range items {
			if !isDiscoverable(s) {
				continue
			}
			if seen[s.Name] {
				continue
			}
			l.applyEnabled(s)
			seen[s.Name] = true
			l.loadedSkills[s.Name] = s
			result = append(result, s)
		}
	}

	for _, source := range l.fileSources() {
		addSkills(l.listSkillsFromDir(source.path, source.source))
	}

	if l.builtinSkillsDir != "" {
		addSkills(l.listSkillsFromDir(l.builtinSkillsDir, "builtin"))
	} else {
		addSkills(l.listBuiltinEmbeddedSkills())
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

// SetDisabled replaces the set of skills hidden from model-facing surfaces.
func (l *SkillLoader) SetDisabled(names []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	disabled := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			disabled[name] = true
		}
	}
	l.disabled = disabled
}

// Disabled returns the disabled skill names in stable order.
func (l *SkillLoader) Disabled() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.disabled))
	for name := range l.disabled {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (l *SkillLoader) applyEnabled(skill *Skill) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	skill.Enabled = !l.disabled[skill.Name]
}

func (l *SkillLoader) fileSources() []skillSource {
	var sources []skillSource
	seen := map[string]bool{}
	add := func(path string, source string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		key, err := filepath.Abs(path)
		if err != nil {
			key = filepath.Clean(path)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		sources = append(sources, skillSource{path: path, source: source})
	}

	if l.workspaceDir != "" {
		add(l.workspaceDir, "workspace")
		workspaceRoot := filepath.Dir(l.workspaceDir)
		add(filepath.Join(workspaceRoot, ".agents", "skills"), "project")
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".agents", "skills"), "user")
	}

	return sources
}

func isDiscoverable(s *Skill) bool {
	return s != nil && s.Name != "" && strings.TrimSpace(s.Description) != ""
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
		result = append(result, skill)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result
}

// LoadSkill loads a specific skill by name
func (l *SkillLoader) LoadSkill(name string) *Skill {
	for _, skill := range l.ListSkills(false) {
		if skill.Name == name {
			return skill
		}
	}
	return nil
}

func (l *SkillLoader) listBuiltinEmbeddedSkills() []*Skill {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := make([]*Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join("builtin", entry.Name(), "SKILL.md")
		data, err := builtinFS.ReadFile(skillPath)
		if err != nil {
			continue
		}
		skill, err := ParseSkillContent(string(data), skillPath)
		if err != nil {
			continue
		}
		skill.Source = "builtin"
		result = append(result, skill)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
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
		if s.DisableModelInvocation {
			continue
		}
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

	if len(lines) == 1 {
		return ""
	}

	lines = append(lines, "</skills>")
	return "## Available Skills\n\nThe following Agent Skills are available. Load the full SKILL.md before following a skill's instructions.\n\n" + joinStrings(lines, "\n")
}

// GetAlwaysSkills returns skills marked as always=true that are available
func (l *SkillLoader) GetAlwaysSkills() []string {
	var result []string

	for _, s := range l.ListSkills(true) {
		if !s.DisableModelInvocation && (s.Always || s.Metadata.Always) {
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
