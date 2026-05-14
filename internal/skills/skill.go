package skills

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents an agent skill with instructions and metadata
type Skill struct {
	Name                   string        `json:"name"`
	Description            string        `json:"description"`
	Homepage               string        `json:"homepage,omitempty"`
	License                string        `json:"license,omitempty"`
	Compatibility          []string      `json:"compatibility,omitempty"`
	Always                 bool          `json:"always,omitempty"`
	Metadata               SkillMetadata `json:"metadata,omitempty"`
	AllowedTools           []string      `json:"allowed_tools,omitempty"`
	DisableModelInvocation bool          `json:"disable_model_invocation,omitempty"`
	Content                string        `json:"content,omitempty"`
	Path                   string        `json:"path,omitempty"`
	Source                 string        `json:"source,omitempty"` // "workspace", "project", "user", or "builtin"
	Available              bool          `json:"available,omitempty"`
	Enabled                bool          `json:"enabled"`
	MissingDeps            []string      `json:"missing_deps,omitempty"`
	Warnings               []string      `json:"warnings,omitempty"`
}

// SkillMetadata contains ori-specific metadata
type SkillMetadata struct {
	Emoji    string            `json:"emoji,omitempty"`
	Requires SkillRequirements `json:"requires,omitempty"`
	Install  []SkillInstall    `json:"install,omitempty"`
	Always   bool              `json:"always,omitempty"`
	OS       []string          `json:"os,omitempty"`
}

// SkillRequirements specifies binary and environment dependencies
type SkillRequirements struct {
	Bins []string `json:"bins,omitempty"`
	Env  []string `json:"env,omitempty"`
}

// SkillInstall provides installation instructions for dependencies
type SkillInstall struct {
	ID      string   `json:"id,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Formula string   `json:"formula,omitempty"`
	Package string   `json:"package,omitempty"`
	Bins    []string `json:"bins,omitempty"`
	Label   string   `json:"label,omitempty"`
}

// skillFrontmatter represents the YAML frontmatter of a SKILL.md file
type skillFrontmatter struct {
	Name                   string   `yaml:"name"`
	Description            string   `yaml:"description"`
	Homepage               string   `yaml:"homepage,omitempty"`
	License                string   `yaml:"license,omitempty"`
	Compatibility          []string `yaml:"compatibility,omitempty"`
	Always                 bool     `yaml:"always,omitempty"`
	Metadata               any      `yaml:"metadata,omitempty"`
	AllowedTools           []string `yaml:"allowed-tools,omitempty"`
	DisableModelInvocation bool     `yaml:"disable-model-invocation,omitempty"`
}

// ParseSkill parses a SKILL.md file and returns a Skill
func ParseSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSkillContent(string(data), path)
}

// ParseSkillContent parses SKILL.md content and returns a Skill
func ParseSkillContent(content string, path string) (*Skill, error) {
	skill := &Skill{
		Path:    path,
		Content: content,
		Enabled: true,
	}

	// Parse frontmatter
	frontmatter, body, err := parseFrontmatter(content)
	if err != nil {
		return nil, err
	}

	skill.Name = frontmatter.Name
	skill.Description = frontmatter.Description
	skill.Homepage = frontmatter.Homepage
	skill.License = frontmatter.License
	skill.Compatibility = append([]string(nil), frontmatter.Compatibility...)
	skill.Always = frontmatter.Always
	skill.AllowedTools = append([]string(nil), frontmatter.AllowedTools...)
	skill.DisableModelInvocation = frontmatter.DisableModelInvocation

	// Parse metadata JSON
	if frontmatter.Metadata != nil {
		meta, err := parseOriMetadata(frontmatter.Metadata)
		if err == nil {
			skill.Metadata = meta
		} else {
			skill.Warnings = append(skill.Warnings, "metadata: "+err.Error())
		}
	}

	// Strip frontmatter from content for the body
	skill.Content = body
	skill.Warnings = append(skill.Warnings, validateSkillMetadata(skill)...)

	// Check availability
	skill.Available = skill.checkRequirements()
	skill.MissingDeps = skill.getMissingDependencies()

	return skill, nil
}

// parseFrontmatter extracts YAML frontmatter and body from SKILL.md content
func parseFrontmatter(content string) (*skillFrontmatter, string, error) {
	frontmatter := &skillFrontmatter{}

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return frontmatter, content, nil
	}

	// Find end of frontmatter
	endIdx := strings.Index(normalized[4:], "\n---")
	if endIdx < 0 {
		return frontmatter, content, nil
	}
	endIdx += 4 // Account for the opening "---\n"

	frontmatterContent := normalized[4:endIdx]
	body := normalized[endIdx+4:]
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}

	if err := yaml.Unmarshal([]byte(frontmatterContent), frontmatter); err != nil {
		return nil, content, err
	}

	return frontmatter, body, nil
}

// parseOriMetadata parses the metadata JSON from frontmatter
func parseOriMetadata(raw any) (SkillMetadata, error) {
	var data map[string]json.RawMessage
	meta := SkillMetadata{}

	normalized := normalizeYAMLValue(raw)
	if text, ok := normalized.(string); ok {
		if err := json.Unmarshal([]byte(text), &data); err != nil {
			return meta, err
		}
	} else {
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return meta, err
		}
		if err := json.Unmarshal(encoded, &data); err != nil {
			return meta, err
		}
	}

	// Handle top-level ori or openclaw wrapper
	if oriData, ok := data["ori"]; ok {
		if err := json.Unmarshal(oriData, &data); err == nil {
			delete(data, "ori")
		}
	} else if openclawData, ok := data["openclaw"]; ok {
		if err := json.Unmarshal(openclawData, &data); err == nil {
			delete(data, "openclaw")
		}
	}

	// Parse requires
	if requiresRaw, ok := data["requires"]; ok {
		var requires SkillRequirements
		if err := json.Unmarshal(requiresRaw, &requires); err == nil {
			meta.Requires = requires
		}
	}

	// Parse emoji
	if emojiRaw, ok := data["emoji"]; ok {
		var emoji string
		if err := json.Unmarshal(emojiRaw, &emoji); err == nil {
			meta.Emoji = emoji
		}
	}

	// Parse os
	if osRaw, ok := data["os"]; ok {
		var osList []string
		if err := json.Unmarshal(osRaw, &osList); err == nil {
			meta.OS = osList
		}
	}

	// Parse install
	if installRaw, ok := data["install"]; ok {
		var install []SkillInstall
		if err := json.Unmarshal(installRaw, &install); err == nil {
			meta.Install = install
		}
	}

	// Parse always
	if alwaysRaw, ok := data["always"]; ok {
		var always bool
		if err := json.Unmarshal(alwaysRaw, &always); err == nil {
			meta.Always = always
		}
	}

	return meta, nil
}

func normalizeYAMLValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = normalizeYAMLValue(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[fmt.Sprint(k)] = normalizeYAMLValue(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = normalizeYAMLValue(v)
		}
		return out
	default:
		return value
	}
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validateSkillMetadata(skill *Skill) []string {
	var warnings []string
	if strings.TrimSpace(skill.Name) == "" {
		warnings = append(warnings, "missing name")
	} else if !skillNamePattern.MatchString(skill.Name) {
		warnings = append(warnings, "name should use lowercase letters, digits, and hyphens")
	}
	if strings.TrimSpace(skill.Description) == "" {
		warnings = append(warnings, "missing description")
	}
	dirName := filepath.Base(filepath.Dir(skill.Path))
	if dirName != "." && dirName != "" && skill.Name != "" && dirName != skill.Name {
		warnings = append(warnings, "directory name does not match skill name")
	}
	if len(skill.Description) > 240 {
		warnings = append(warnings, "description is longer than 240 characters")
	}
	return warnings
}

// checkRequirements verifies if skill dependencies are met
func (s *Skill) checkRequirements() bool {
	// Check OS requirement
	if len(s.Metadata.OS) > 0 {
		supported := false
		for _, os := range s.Metadata.OS {
			if strings.Contains(strings.ToLower(runtimeGOOS()), strings.ToLower(os)) {
				supported = true
				break
			}
		}
		if !supported {
			return false
		}
	}

	// Check binary dependencies
	for _, bin := range s.Metadata.Requires.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			return false
		}
	}

	// Check environment variables
	for _, env := range s.Metadata.Requires.Env {
		if os.Getenv(env) == "" {
			return false
		}
	}

	return true
}

// getMissingDependencies returns list of missing dependencies
func (s *Skill) getMissingDependencies() []string {
	var missing []string

	// Check OS requirement
	if len(s.Metadata.OS) > 0 {
		supported := false
		for _, os := range s.Metadata.OS {
			if strings.Contains(strings.ToLower(runtimeGOOS()), strings.ToLower(os)) {
				supported = true
				break
			}
		}
		if !supported {
			missing = append(missing, "OS: "+strings.Join(s.Metadata.OS, ","))
		}
	}

	// Check binary dependencies
	for _, bin := range s.Metadata.Requires.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, "CLI: "+bin)
		}
	}

	// Check environment variables
	for _, env := range s.Metadata.Requires.Env {
		if os.Getenv(env) == "" {
			missing = append(missing, "ENV: "+env)
		}
	}

	return missing
}

// GetContent returns the skill content without frontmatter
func (s *Skill) GetContent() string {
	return s.Content
}

// GetInstallInstructions returns formatted installation instructions
func (s *Skill) GetInstallInstructions() string {
	if len(s.Metadata.Install) == 0 {
		return ""
	}

	var lines []string
	for _, inst := range s.Metadata.Install {
		if inst.Label != "" {
			lines = append(lines, inst.Label)
		} else if inst.Formula != "" {
			lines = append(lines, "brew install "+inst.Formula)
		} else if inst.Package != "" {
			lines = append(lines, "apt install "+inst.Package)
		}
	}
	return strings.Join(lines, "\n")
}

// skillFileInfo holds skill directory info for listing
type skillFileInfo struct {
	Name   string
	Path   string
	Source string
}

// ListSkillDirs returns skill directories in a base path
func ListSkillDirs(basePath string) ([]skillFileInfo, error) {
	var skills []skillFileInfo

	if _, err := os.Stat(basePath); err != nil {
		if os.IsNotExist(err) {
			return skills, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(basePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path == basePath {
			return nil
		}
		skillFile := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			skills = append(skills, skillFileInfo{
				Name:   entry.Name(),
				Path:   skillFile,
				Source: "directory",
			})
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return skills, nil
}

// runtimeGOOS returns runtime.GOOS
func runtimeGOOS() string {
	return runtime.GOOS
}
