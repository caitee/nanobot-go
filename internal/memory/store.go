package memory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Store owns the two memory files. It is safe for concurrent use because all
// operations are independent file operations; callers that need to
// serialize writes should do so externally.
type Store struct {
	dir         string
	memoryFile  string
	historyFile string
}

// NewStore creates (or opens) the memory directory under workspace.
func NewStore(workspace string) *Store {
	dir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("memory: failed to create directory", "error", err)
	}
	return &Store{
		dir:         dir,
		memoryFile:  filepath.Join(dir, "MEMORY.md"),
		historyFile: filepath.Join(dir, "HISTORY.md"),
	}
}

// Dir returns the memory directory path.
func (s *Store) Dir() string { return s.dir }

// MemoryFile returns the path to MEMORY.md.
func (s *Store) MemoryFile() string { return s.memoryFile }

// HistoryFile returns the path to HISTORY.md.
func (s *Store) HistoryFile() string { return s.historyFile }

// ReadLongTerm returns the contents of MEMORY.md or "" if it doesn't exist.
func (s *Store) ReadLongTerm() string {
	data, err := os.ReadFile(s.memoryFile)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("memory: failed to read MEMORY.md", "error", err)
		}
		return ""
	}
	return string(data)
}

// WriteLongTerm replaces the contents of MEMORY.md.
func (s *Store) WriteLongTerm(content string) error {
	if err := os.WriteFile(s.memoryFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write MEMORY.md: %w", err)
	}
	return nil
}

// AppendHistory adds an entry to HISTORY.md. The entry is separated from
// previous content by a blank line.
func (s *Store) AppendHistory(entry string) error {
	f, err := os.OpenFile(s.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memory: open HISTORY.md: %w", err)
	}
	defer f.Close()
	if len(entry) > 0 && entry[len(entry)-1] != '\n' {
		entry += "\n"
	}
	if _, err := f.WriteString(entry + "\n"); err != nil {
		return fmt.Errorf("memory: write HISTORY.md: %w", err)
	}
	return nil
}

// PromptFragment returns a markdown fragment suitable for inclusion in the
// system prompt. Returns "" when MEMORY.md is empty.
func (s *Store) PromptFragment() string {
	lt := s.ReadLongTerm()
	if lt == "" {
		return ""
	}
	return "## Long-term Memory\n\n" + lt
}
