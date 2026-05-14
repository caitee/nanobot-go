package skills

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const maxSkillListDescriptionRunes = 140

// FormatSkillList renders available skills as slash-command help text.
func FormatSkillList(items []*Skill) string {
	if len(items) == 0 {
		return "No skills found."
	}
	sorted := append([]*Skill(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	blocks := []string{"Available skills:"}
	for _, skill := range sorted {
		if skill == nil || skill.Name == "" {
			continue
		}
		status := "available"
		if !skill.Available {
			status = "unavailable"
		}
		if !skill.Enabled {
			status = "disabled"
		}
		parts := []string{
			fmt.Sprintf("/skill:%s", skill.Name),
			fmt.Sprintf("[%s]", firstNonEmpty(skill.Source, "unknown")),
			status,
		}
		if skill.Description != "" {
			parts = append(parts, "- "+truncateSkillDescription(skill.Description))
		}
		if len(skill.MissingDeps) > 0 {
			parts = append(parts, "(missing: "+formatMissingDeps(skill.MissingDeps)+")")
		}
		blocks = append(blocks, strings.Join(parts, " "))
	}
	if len(blocks) == 1 {
		return "No skills found."
	}
	return strings.Join(blocks, "\n\n")
}

// ExpandSkillCommand expands /skill:<name> into the skill body plus arguments.
func ExpandSkillCommand(loader *SkillLoader, text string) (string, bool) {
	if loader == nil || !strings.HasPrefix(text, "/skill:") {
		return text, false
	}
	spaceIdx := strings.IndexByte(text, ' ')
	name := strings.TrimSpace(strings.TrimPrefix(text, "/skill:"))
	args := ""
	if spaceIdx >= 0 {
		name = strings.TrimSpace(strings.TrimPrefix(text[:spaceIdx], "/skill:"))
		args = strings.TrimSpace(text[spaceIdx+1:])
	}
	if name == "" {
		return text, false
	}
	skill := loader.LoadSkill(name)
	if skill == nil {
		return text, false
	}
	body := strings.TrimSpace(skill.Content)
	location := skill.Path
	baseDir := filepath.Dir(location)
	block := fmt.Sprintf("<skill name=%q location=%q>\nReferences are relative to %s.\n\n%s\n</skill>", skill.Name, location, baseDir, body)
	if args != "" {
		return block + "\n\nUser: " + args, true
	}
	return block, true
}

func formatMissingDeps(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch {
		case strings.HasPrefix(item, "CLI: "):
			out = append(out, "bin:"+strings.TrimPrefix(item, "CLI: "))
		case strings.HasPrefix(item, "ENV: "):
			out = append(out, "env:"+strings.TrimPrefix(item, "ENV: "))
		case strings.HasPrefix(item, "OS: "):
			out = append(out, "os:"+strings.TrimPrefix(item, "OS: "))
		default:
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncateSkillDescription(description string) string {
	description = strings.Join(strings.Fields(description), " ")
	runes := []rune(description)
	if len(runes) <= maxSkillListDescriptionRunes {
		return description
	}
	if maxSkillListDescriptionRunes <= 3 {
		return string(runes[:maxSkillListDescriptionRunes])
	}
	return strings.TrimSpace(string(runes[:maxSkillListDescriptionRunes-3])) + "..."
}
