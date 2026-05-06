package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreReadWriteAppend(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	if got := s.ReadLongTerm(); got != "" {
		t.Fatalf("initial MEMORY.md should be empty, got %q", got)
	}
	if got := s.PromptFragment(); got != "" {
		t.Fatalf("initial fragment should be empty, got %q", got)
	}

	if err := s.WriteLongTerm("fact one\n"); err != nil {
		t.Fatalf("WriteLongTerm: %v", err)
	}
	if got := s.ReadLongTerm(); got != "fact one\n" {
		t.Fatalf("ReadLongTerm = %q", got)
	}
	frag := s.PromptFragment()
	if !strings.Contains(frag, "Long-term Memory") || !strings.Contains(frag, "fact one") {
		t.Fatalf("unexpected fragment: %q", frag)
	}

	if err := s.AppendHistory("[2026-05-06 12:00] first entry"); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "memory", "HISTORY.md"))
	if err != nil {
		t.Fatalf("read HISTORY.md: %v", err)
	}
	if !strings.Contains(string(data), "first entry") {
		t.Fatalf("HISTORY missing entry: %q", string(data))
	}
}
