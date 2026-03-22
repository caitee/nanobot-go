package skills

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Skill represents an agent skill with instructions and metadata
type Skill struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Homepage        string            `json:"homepage,omitempty"`
	Always          bool              `json:"always,omitempty"`
	Metadata        SkillMetadata     `json:"metadata,omitempty"`
	Content         string            `json:"content,omitempty"`
	Path            string            `json:"path,omitempty"`
	Source          string            `json:"source,omitempty"` // "workspace" or "builtin"
	Available       bool              `json:"available,omitempty"`
	MissingDeps     []string          `json:"missing_deps,omitempty"`
}

// SkillMetadata contains nanobot-specific metadata
type SkillMetadata struct {
	Emoji     string            `json:"emoji,omitempty"`
	Requires  SkillRequirements `json:"requires,omitempty"`
	Install   []SkillInstall    `json:"install,omitempty"`
	Always    bool              `json:"always,omitempty"`
	OS        []string           `json:"os,omitempty"`
}

// SkillRequirements specifies binary and environment dependencies
type SkillRequirements struct {
	Bins []string `json:"bins,omitempty"`
	Env  []string `json:"env,omitempty"`
}

// SkillInstall provides installation instructions for dependencies
type SkillInstall struct {
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Formula  string `json:"formula,omitempty"`
	Package  string `json:"package,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Label    string `json:"label,omitempty"`
}

// skillFrontmatter represents the YAML frontmatter of a SKILL.md file
type skillFrontmatter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Homepage    string `json:"homepage,omitempty"`
	Always      bool   `json:"always,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
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
	}

	// Parse frontmatter
	frontmatter, body, err := parseFrontmatter(content)
	if err != nil {
		return nil, err
	}

	skill.Name = frontmatter.Name
	skill.Description = frontmatter.Description
	skill.Homepage = frontmatter.Homepage
	skill.Always = frontmatter.Always

	// Parse metadata JSON
	if frontmatter.Metadata != "" {
		meta, err := parseNanobotMetadata(frontmatter.Metadata)
		if err == nil {
			skill.Metadata = meta
		}
	}

	// Strip frontmatter from content for the body
	skill.Content = body

	// Check availability
	skill.Available = skill.checkRequirements()
	skill.MissingDeps = skill.getMissingDependencies()

	return skill, nil
}

// parseFrontmatter extracts YAML frontmatter and body from SKILL.md content
func parseFrontmatter(content string) (*skillFrontmatter, string, error) {
	frontmatter := &skillFrontmatter{}

	if !strings.HasPrefix(content, "---") {
		return frontmatter, content, nil
	}

	// Find end of frontmatter
	endIdx := strings.Index(content[3:], "---")
	if endIdx < 0 {
		return frontmatter, content, nil
	}
	endIdx += 3 // Account for the opening ---

	frontmatterContent := content[3:endIdx]
	body := content[endIdx+3:]

	// Parse frontmatter lines
	for _, line := range strings.Split(frontmatterContent, "\n") {
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"")

			switch key {
			case "name":
				frontmatter.Name = value
			case "description":
				frontmatter.Description = value
			case "homepage":
				frontmatter.Homepage = value
			case "always":
				frontmatter.Always = value == "true" || value == "yes"
			case "metadata":
				frontmatter.Metadata = value
			}
		}
	}

	return frontmatter, body, nil
}

// parseNanobotMetadata parses the metadata JSON from frontmatter
func parseNanobotMetadata(raw string) (SkillMetadata, error) {
	var data map[string]json.RawMessage
	meta := SkillMetadata{}

	// Try to parse as JSON object
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		// Try extracting nanobot or openclaw key
		re := regexp.MustCompile(`"nanobot"\s*:\s*(\{.*?\})\s*(?:,|\})`)
		match := re.FindStringSubmatch(raw)
		if len(match) < 2 {
			return meta, err
		}
		raw = match[1]
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return meta, err
		}
	}

	// Handle top-level nanobot or openclaw wrapper
	if nanobotData, ok := data["nanobot"]; ok {
		if err := json.Unmarshal(nanobotData, &data); err == nil {
			delete(data, "nanobot")
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
	Name    string
	Path    string
	Source  string
}

// ListSkillDirs returns skill directories in a base path
func ListSkillDirs(basePath string) ([]skillFileInfo, error) {
	var skills []skillFileInfo

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return skills, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(basePath, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			skills = append(skills, skillFileInfo{
				Name:   entry.Name(),
				Path:   skillFile,
				Source: "directory",
			})
		}
	}

	return skills, nil
}

// runtimeGOOS returns runtime.GOOS
func runtimeGOOS() string {
	return runtime.GOOS
}
