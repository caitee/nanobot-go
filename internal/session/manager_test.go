package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileSessionStoreListScansDiskAndSortsByUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSessionStore(dir)
	if err != nil {
		t.Fatalf("NewFileSessionStore: %v", err)
	}

	older := &Session{
		Key:       "cli:older",
		CreatedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC),
		Metadata:  map[string]any{},
		Messages: []Message{
			{Role: "user", Content: "first question"},
			{Role: "assistant", Content: "first answer"},
		},
	}
	newer := &Session{
		Key:       "cli:newer",
		CreatedAt: time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 15, 9, 2, 0, 0, time.UTC),
		Metadata:  map[string]any{},
		Messages: []Message{
			{Role: "user", Content: "line one\nline two"},
			{Role: "assistant", Content: "answer"},
			{Role: "user", Content: "latest user prompt"},
		},
	}
	if err := store.Save(older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatalf("Save newer: %v", err)
	}

	reloaded, err := NewFileSessionStore(dir)
	if err != nil {
		t.Fatalf("NewFileSessionStore reload: %v", err)
	}
	infos := reloaded.List()

	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions from disk, got %+v", infos)
	}
	if infos[0].Key != "cli:newer" || infos[1].Key != "cli:older" {
		t.Fatalf("expected newest first, got %+v", infos)
	}
	if infos[0].MessageCount != 3 {
		t.Fatalf("newer message count = %d; want 3", infos[0].MessageCount)
	}
	if infos[0].LastMessagePreview != "latest user prompt" {
		t.Fatalf("newer preview = %q; want latest user prompt", infos[0].LastMessagePreview)
	}
}

func TestFileSessionStoreListMergesMemoryAndDiskWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSessionStore(dir)
	if err != nil {
		t.Fatalf("NewFileSessionStore: %v", err)
	}

	sess := store.GetOrCreate("cli:cached")
	sess.CreatedAt = time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	sess.UpdatedAt = time.Date(2026, 5, 15, 8, 1, 0, 0, time.UTC)
	sess.Messages = append(sess.Messages, Message{Role: "user", Content: "cached prompt"})
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save cached: %v", err)
	}

	infos := store.List()

	if len(infos) != 1 {
		t.Fatalf("expected one merged session, got %+v", infos)
	}
	if infos[0].Key != "cli:cached" {
		t.Fatalf("key = %q; want cli:cached", infos[0].Key)
	}
	if infos[0].LastMessagePreview != "cached prompt" {
		t.Fatalf("preview = %q; want cached prompt", infos[0].LastMessagePreview)
	}
}

func TestFileSessionStoreListUsesFilenameWhenMetadataKeyMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "legacy-name.jsonl"), []byte(`{"role":"user","content":"legacy prompt"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err := NewFileSessionStore(dir)
	if err != nil {
		t.Fatalf("NewFileSessionStore: %v", err)
	}

	infos := store.List()

	if len(infos) != 1 {
		t.Fatalf("expected one legacy session, got %+v", infos)
	}
	if infos[0].Key != "legacy-name" {
		t.Fatalf("key = %q; want legacy-name", infos[0].Key)
	}
	if infos[0].LastMessagePreview != "legacy prompt" {
		t.Fatalf("preview = %q; want legacy prompt", infos[0].LastMessagePreview)
	}
}
