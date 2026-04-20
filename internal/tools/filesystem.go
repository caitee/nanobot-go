package tools

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png" // for image decode
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Filesystem tools: read_file, write_file, edit_file, list_dir

type fsTool struct {
	BaseTool
	workspace      string
	allowedDir     string
	ignorePatterns map[string]bool
}

func newFsTool(workspace, allowedDir string) *fsTool {
	return &fsTool{
		workspace:      workspace,
		allowedDir:     allowedDir,
		ignorePatterns: defaultIgnorePatterns(),
	}
}

func defaultIgnorePatterns() map[string]bool {
	patterns := []string{
		".git", "node_modules", "__pycache__", ".venv", "venv",
		"dist", "build", ".tox", ".mypy_cache", ".pytest_cache",
		".ruff_cache", ".coverage", "htmlcov", ".idea", ".vscode",
	}
	m := make(map[string]bool)
	for _, p := range patterns {
		m[p] = true
	}
	return m
}

func (t *fsTool) resolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Handle relative paths
	if !filepath.IsAbs(path) && t.workspace != "" {
		path = filepath.Join(t.workspace, path)
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Security: check allowed directories
	if t.allowedDir != "" {
		allowedAbs, _ := filepath.Abs(t.allowedDir)
		absPathResolved, _ := filepath.Abs(absPath)
		if !strings.HasPrefix(absPathResolved, allowedAbs) {
			return "", fmt.Errorf("path %s is outside allowed directory %s", path, t.allowedDir)
		}
	}

	return absPath, nil
}

func (t *fsTool) shouldIgnore(name string, isDir bool) bool {
	if t.ignorePatterns[name] {
		return true
	}
	// Check for common ignored prefixes/suffixes
	if strings.HasPrefix(name, ".") && name != ".gitignore" {
		return true
	}
	return false
}

// ============================================================================
// ReadFileTool
// ============================================================================

type ReadFileTool struct {
	*fsTool
}

func NewReadFileTool(workspace, allowedDir string) *ReadFileTool {
	return &ReadFileTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *ReadFileTool) Name() string    { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read the contents of a text or image file. Returns numbered lines with pagination support. Supports UTF-8 text and common image formats. Use offset and limit for large files."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "The file path to read"},
			"offset":  map[string]any{"type": "integer", "description": "Line number to start reading from (1-indexed, default 1)", "minimum": 1},
			"limit":   map[string]any{"type": "integer", "description": "Maximum number of lines to read (default 2000)", "minimum": 1},
		},
		"required": []any{"path"},
		"examples": []any{
			map[string]any{"path": "README.md"},
			map[string]any{"path": "src/main.go", "offset": 1, "limit": 100},
			map[string]any{"path": "docs/api.md"},
		},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	offset, _ := params["offset"].(int)
	limit, _ := params["limit"].(int)

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: File not found: %s", path), nil
		}
		return nil, err
	}

	if info.IsDir() {
		return fmt.Sprintf("Error: Not a file: %s", path), nil
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}

	// Check if it's an image
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" && len(raw) > 32 {
		// Try to detect image format from magic bytes
		if img, _, err := image.Decode(bytes.NewReader(raw[:min(32, len(raw))])); err == nil {
			switch img.(type) {
			case *image.RGBA:
				mimeType = "image/png"
			case *image.NRGBA:
				mimeType = "image/png"
			case *image.YCbCr:
				mimeType = "image/jpeg"
			case *image.Gray:
				mimeType = "image/png"
			}
		}
	}

	if mimeType != "" && strings.HasPrefix(mimeType, "image/") {
		// Return image as base64
		return map[string]any{
			"type":    "image",
			"mime":    mimeType,
			"data":    raw,
			"caption": fmt.Sprintf("(Image file: %s)", path),
		}, nil
	}

	// Try UTF-8 text
	text := string(raw)
	if !isValidUTF8(raw) {
		return fmt.Sprintf("Error: Cannot read binary file %s (MIME: %s). Only UTF-8 text and images are supported.", path, mimeType), nil
	}

	lines := strings.Split(text, "\n")
	total := len(lines)

	if offset < 1 {
		offset = 1
	}
	if offset > total {
		return fmt.Sprintf("Error: offset %d is beyond end of file (%d lines)", offset, total), nil
	}

	start := offset - 1
	limitLines := limit
	if limitLines <= 0 {
		limitLines = 2000
	}
	end := min(start+limitLines, total)

	// Build numbered lines
	var buf bytes.Buffer
	maxLineNum := len(strconv.Itoa(end))
	format := fmt.Sprintf("%%%dd| %%s\n", maxLineNum)
	for i := start; i < end; i++ {
		buf.WriteString(fmt.Sprintf(format, i+1, lines[i]))
	}

	result := buf.String()

	// Truncate if too long
	maxChars := 128000
	if len(result) > maxChars {
		result = result[:maxChars] + "\n...(truncated)"
	}

	if end < total {
		result += fmt.Sprintf("\n\n(Showing lines %d-%d of %d. Use offset=%d to continue.)", offset, end, total, end+1)
	} else {
		result += fmt.Sprintf("\n\n(End of file — %d lines total)", total)
	}

	return result, nil
}

func isValidUTF8(data []byte) bool {
	return utf8.Valid(data)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// WriteFileTool
// ============================================================================

type WriteFileTool struct {
	*fsTool
}

func NewWriteFileTool(workspace, allowedDir string) *WriteFileTool {
	return &WriteFileTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *WriteFileTool) Name() string    { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Create or overwrite a file with the given content. Automatically creates parent directories if they don't exist. Use this to create new files or update existing ones."
}

func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "The file path to write to"},
			"content": map[string]any{"type": "string", "description": "The content to write"},
		},
		"required": []any{"path", "content"},
		"examples": []any{
			map[string]any{"path": "output.txt", "content": "Hello, World!"},
			map[string]any{"path": "src/config.go", "content": "package main\n\nconst Version = \"1.0.0\""},
		},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	// Create parent directories
	parent := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %w", err)
	}

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return nil, err
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), resolvedPath), nil
}

// ============================================================================
// EditFileTool
// ============================================================================

type EditFileTool struct {
	*fsTool
}

func NewEditFileTool(workspace, allowedDir string) *EditFileTool {
	return &EditFileTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *EditFileTool) Name() string    { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Precisely edit a file by replacing a specific section (old_text) with new content. Handles whitespace differences. Use replace_all=true to replace every occurrence. Always use exact text from the file - include surrounding context to make the match unique."
}

func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "The file path to edit"},
			"old_text":   map[string]any{"type": "string", "description": "Exact text to find (must match file content)"},
			"new_text":   map[string]any{"type": "string", "description": "Replacement text"},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default false)"},
		},
		"required": []any{"path", "old_text", "new_text"},
		"examples": []any{
			map[string]any{"path": "config.go", "old_text": "MaxRetries = 3", "new_text": "MaxRetries = 10"},
			map[string]any{"path": "main.go", "old_text": "fmt.Println(\"debug\")", "new_text": "// Debug removed"},
		},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	oldText, _ := params["old_text"].(string)
	newText, _ := params["new_text"].(string)
	replaceAll, _ := params["replace_all"].(bool)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if oldText == "" {
		return nil, fmt.Errorf("old_text is required")
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		return fmt.Sprintf("Error: File not found: %s", path), nil
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}

	// Normalize line endings
	content := normalizeLineEndings(string(raw))
	oldTextNorm := normalizeLineEndings(oldText)
	newTextNorm := normalizeLineEndings(newText)

	// Find match
	match, count := findMatch(content, oldTextNorm)

	if match == "" {
		return t.notFoundMsg(oldText, content, path), nil
	}

	if count > 1 && !replaceAll {
		return fmt.Sprintf("Warning: old_text appears %d times. Provide more context to make it unique, or set replace_all=true.", count), nil
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, match, newTextNorm)
	} else {
		newContent = strings.Replace(content, match, newTextNorm, 1)
	}

	// Restore original line endings if needed
	usesCRLF := bytes.Contains(raw, []byte{'\r', '\n'})
	if usesCRLF {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	}

	if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
		return nil, err
	}

	return fmt.Sprintf("Successfully edited %s", resolvedPath), nil
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func findMatch(content, oldText string) (string, int) {
	// Exact match first
	if strings.Contains(content, oldText) {
		return oldText, strings.Count(content, oldText)
	}

	// Line-stripped matching
	oldLines := strings.Split(oldText, "\n")
	if len(oldLines) == 0 {
		return "", 0
	}

	strippedOld := make([]string, len(oldLines))
	for i, l := range oldLines {
		strippedOld[i] = strings.TrimSpace(l)
	}

	contentLines := strings.Split(content, "\n")
	candidates := make([]string, 0)

	for i := 0; i <= len(contentLines)-len(strippedOld); i++ {
		window := contentLines[i : i+len(strippedOld)]
		strippedWindow := make([]string, len(window))
		for j, l := range window {
			strippedWindow[j] = strings.TrimSpace(l)
		}

		found := true
		for j, so := range strippedOld {
			if strippedWindow[j] != so {
				found = false
				break
			}
		}
		if found {
			candidates = append(candidates, strings.Join(window, "\n"))
		}
	}

	if len(candidates) > 0 {
		return candidates[0], len(candidates)
	}
	return "", 0
}

func (t *EditFileTool) notFoundMsg(oldText, content, path string) string {
	lines := strings.Split(content, "\n")
	oldLines := strings.Split(oldText, "\n")
	window := len(oldLines)

	if window <= 0 || len(lines) == 0 {
		return fmt.Sprintf("Error: old_text not found in %s. Verify the file content.", path)
	}

	// Find best match using similarity
	bestRatio := 0.0
	bestStart := 0

	for i := 0; i <= max(1, len(lines)-window); i++ {
		w := lines[i : i+window]
		ratio := similarity(oldLines, w)
		if ratio > bestRatio {
			bestRatio = ratio
			bestStart = i
		}
	}

	if bestRatio > 0.5 {
		diff := unifiedDiff(oldLines, lines[bestStart:bestStart+window], path)
		return fmt.Sprintf("Error: old_text not found in %s.\nBest match (%d%% similar) at line %d:\n%s",
			path, int(bestRatio*100), bestStart+1, diff)
	}
	return fmt.Sprintf("Error: old_text not found in %s. No similar text found. Verify the file content.", path)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func similarity(a, b []string) float64 {
	matches := 0
	minLen := min(len(a), len(b))
	for i := 0; i < minLen; i++ {
		if strings.TrimSpace(a[i]) == strings.TrimSpace(b[i]) {
			matches++
		}
	}
	return float64(matches) / float64(max(len(a), len(b)))
}

func unifiedDiff(oldLines, newLines []string, path string) string {
	var buf bytes.Buffer
	bufs := []byte(fmt.Sprintf("--- old_text (provided)\n+++ %s (actual)\n", path))

	for i, l := range oldLines {
		if i < len(bufs) {
			buf.WriteByte('-')
		} else {
			buf.WriteByte(' ')
		}
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	for i, l := range newLines {
		if i < len(bufs) {
			buf.WriteByte('+')
		} else {
			buf.WriteByte(' ')
		}
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	return buf.String()
}

// ============================================================================
// ListDirTool
// ============================================================================

type ListDirTool struct {
	*fsTool
}

func NewListDirTool(workspace, allowedDir string) *ListDirTool {
	return &ListDirTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *ListDirTool) Name() string    { return "list_dir" }
func (t *ListDirTool) Description() string {
	return "List files and subdirectories in a folder. Use recursive=true to see the full tree. Ignores common noise directories (.git, node_modules, __pycache__, etc.)."
}

func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "The directory path to list"},
			"recursive":  map[string]any{"type": "boolean", "description": "Recursively list all files (default false)"},
			"max_entries": map[string]any{"type": "integer", "description": "Maximum entries to return (default 200)", "minimum": 1},
		},
		"required": []any{"path"},
		"examples": []any{
			map[string]any{"path": "."},
			map[string]any{"path": "src", "recursive": true},
			map[string]any{"path": ".", "recursive": true, "max_entries": 50},
		},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	recursive, _ := params["recursive"].(bool)
	maxEntries, _ := params["max_entries"].(int)

	if maxEntries <= 0 {
		maxEntries = 200
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: Directory not found: %s", path), nil
		}
		return nil, err
	}

	if !info.IsDir() {
		return fmt.Sprintf("Error: Not a directory: %s", path), nil
	}

	var entries []string
	total := 0

	if recursive {
		err = filepath.Walk(resolvedPath, func(walkPath string, walkInfo os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}

			relPath, _ := filepath.Rel(resolvedPath, walkPath)
			parts := strings.Split(filepath.ToSlash(relPath), "/")

			// Check if any part should be ignored
			for _, part := range parts {
				if t.shouldIgnore(part, walkInfo.IsDir()) && part != "." {
					if walkInfo.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			total++
			if len(entries) < maxEntries {
				if walkPath != resolvedPath {
					if walkInfo.IsDir() {
						entries = append(entries, relPath+"/")
					} else {
						entries = append(entries, relPath)
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		dirents, err := os.ReadDir(resolvedPath)
		if err != nil {
			return nil, err
		}

		for _, de := range dirents {
			if t.shouldIgnore(de.Name(), de.IsDir()) {
				continue
			}
			total++
			if len(entries) < maxEntries {
				if de.IsDir() {
					entries = append(entries, "📁 "+de.Name())
				} else {
					entries = append(entries, "📄 "+de.Name())
				}
			}
		}
	}

	if len(entries) == 0 && total == 0 {
		return fmt.Sprintf("Directory %s is empty", path), nil
	}

	result := strings.Join(entries, "\n")
	if total > maxEntries {
		result += fmt.Sprintf("\n\n(truncated, showing first %d of %d entries)", maxEntries, total)
	}
	return result, nil
}

// ============================================================================
// GlobTool
// ============================================================================

type GlobTool struct {
	*fsTool
}

func NewGlobTool(workspace, allowedDir string) *GlobTool {
	return &GlobTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *GlobTool) Name() string    { return "glob" }
func (t *GlobTool) Description() string {
	return "Find files by glob pattern. ** matches any path, * matches within a directory component, ? matches a single character. Example: **/*.go finds all Go files recursively."
}

func (t *GlobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "The directory path to search from"},
			"pattern": map[string]any{"type": "string", "description": "Glob pattern (e.g., **/*.go, **/*.py, src/**/*.ts)"},
			"max_results": map[string]any{"type": "integer", "description": "Maximum results to return (default 100)", "minimum": 1},
		},
		"required": []any{"path", "pattern"},
		"examples": []any{
			map[string]any{"path": ".", "pattern": "**/*.go"},
			map[string]any{"path": "src", "pattern": "**/*.test.ts"},
			map[string]any{"path": ".", "pattern": "**/*.json"},
		},
	}
}

func (t *GlobTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	pattern, _ := params["pattern"].(string)
	maxResults, _ := params["max_results"].(int)

	if maxResults <= 0 {
		maxResults = 100
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Sprintf("Error: Path not found: %s", path), nil
	}

	if !info.IsDir() {
		resolvedPath = filepath.Dir(resolvedPath)
	}

	// Convert glob pattern to regex
	regexPattern := globToRegex(pattern)
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	var matches []string
	count := 0

	err = filepath.Walk(resolvedPath, func(walkPath string, walkInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(resolvedPath, walkPath)
		if re.MatchString(filepath.ToSlash(relPath)) {
			count++
			if len(matches) < maxResults {
				if walkInfo.IsDir() {
					matches = append(matches, relPath+"/")
				} else {
					matches = append(matches, relPath)
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No files matching '%s' found in %s", pattern, path), nil
	}

	result := strings.Join(matches, "\n")
	if count > maxResults {
		result += fmt.Sprintf("\n\n(truncated, showing first %d of %d matches)", maxResults, count)
	}
	return result, nil
}

func globToRegex(glob string) string {
	// Simple glob to regex conversion
	// ** matches any path, * matches within a path component, ? matches single char
	regex := regexp.QuoteMeta(glob)
	regex = strings.ReplaceAll(regex, `\*\*`, `.*`)
	regex = strings.ReplaceAll(regex, `\*`, `[^/]*`)
	regex = strings.ReplaceAll(regex, `\?`, `[^/]`)
	return "^" + regex + "$"
}

// ============================================================================
// FindTool
// ============================================================================

type FindTool struct {
	*fsTool
}

func NewFindTool(workspace, allowedDir string) *FindTool {
	return &FindTool{fsTool: newFsTool(workspace, allowedDir)}
}

func (t *FindTool) Name() string    { return "find" }
func (t *FindTool) Description() string {
	return "Grep-style text search across files. Returns file paths and line numbers where the pattern matches. Supports regex. Use file_glob to limit to specific file types."
}

func (t *FindTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "The directory path to search from"},
			"pattern":  map[string]any{"type": "string", "description": "Text or regex pattern to search for"},
			"file_glob": map[string]any{"type": "string", "description": "Limit to files matching glob (e.g., *.go, *.py)"},
			"max_results": map[string]any{"type": "integer", "description": "Maximum results to return (default 50)", "minimum": 1},
		},
		"required": []any{"path", "pattern"},
		"examples": []any{
			map[string]any{"path": "src", "pattern": "TODO"},
			map[string]any{"path": ".", "pattern": "func main", "file_glob": "*.go"},
			map[string]any{"path": "src", "pattern": "error:", "file_glob": "*.go"},
		},
	}
}

func (t *FindTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	path, _ := params["path"].(string)
	pattern, _ := params["pattern"].(string)
	fileGlob, _ := params["file_glob"].(string)
	maxResults, _ := params["max_results"].(int)

	if maxResults <= 0 {
		maxResults = 50
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Sprintf("Error: Path not found: %s", path), nil
	}

	if !info.IsDir() {
		resolvedPath = filepath.Dir(resolvedPath)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	var filePattern *regexp.Regexp
	if fileGlob != "" {
		pattern2 := globToRegex(fileGlob)
		filePattern, err = regexp.Compile(pattern2)
		if err != nil {
			return nil, fmt.Errorf("invalid file glob: %w", err)
		}
	}

	var matches []string
	matchCount := 0

	err = filepath.Walk(resolvedPath, func(walkPath string, walkInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if walkInfo.IsDir() {
			relPath, _ := filepath.Rel(resolvedPath, walkPath)
			if t.shouldIgnore(walkInfo.Name(), true) || strings.HasPrefix(relPath, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(resolvedPath, walkPath)

		// Check file glob
		if filePattern != nil && !filePattern.MatchString(filepath.ToSlash(relPath)) {
			return nil
		}

		// Read and search file
		content, err := os.ReadFile(walkPath)
		if err != nil {
			return nil
		}

		text := string(content)
		lines := strings.Split(text, "\n")

		for i, line := range lines {
			if re.MatchString(line) {
				matchCount++
				if len(matches) < maxResults {
					matches = append(matches, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for '%s' in %s", pattern, path), nil
	}

	result := strings.Join(matches, "\n")
	if matchCount > maxResults {
		result += fmt.Sprintf("\n\n(truncated, showing first %d of %d matches)", maxResults, matchCount)
	}
	return result, nil
}

// ============================================================================
// Backwards compatibility: FilesystemTool (original single tool with actions)
// ============================================================================

type FilesystemTool struct {
	workspace  string
	allowedDirs []string
}

func NewFilesystemTool(allowedDirs []string) *FilesystemTool {
	var workspace string
	if len(allowedDirs) > 0 {
		workspace = allowedDirs[0]
	}
	return &FilesystemTool{
		workspace:  workspace,
		allowedDirs: allowedDirs,
	}
}

func (t *FilesystemTool) Name() string    { return "filesystem" }
func (t *FilesystemTool) Description() string { return "Read and write files" }

func (t *FilesystemTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "enum": []any{"read", "write", "list"}},
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []any{"action", "path"},
	}
}

func (t *FilesystemTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	action, _ := params["action"].(string)
	path, _ := params["path"].(string)

	// Delegate to individual tools
	allowedDir := ""
	if len(t.allowedDirs) > 0 {
		allowedDir = t.allowedDirs[0]
	}
	switch action {
	case "read":
		tool := NewReadFileTool(t.workspace, allowedDir)
		params2 := map[string]any{"path": path}
		return tool.Execute(ctx, params2)
	case "write":
		content, _ := params["content"].(string)
		params2 := map[string]any{"path": path, "content": content}
		tool := NewWriteFileTool(t.workspace, allowedDir)
		return tool.Execute(ctx, params2)
	case "list":
		params2 := map[string]any{"path": path}
		tool := NewListDirTool(t.workspace, allowedDir)
		return tool.Execute(ctx, params2)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}