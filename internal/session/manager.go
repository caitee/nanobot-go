package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionStore defines session persistence interface.
//
// Save rewrites the entire session file. It's the right tool for migrations,
// compaction, or explicit flushes — but on hot paths prefer AppendMessage,
// which appends a single JSONL line.
type SessionStore interface {
	GetOrCreate(key string) *Session
	Save(session *Session) error
	// AppendMessage appends a single message entry to the session file
	// without rewriting existing content. The implementation must ensure
	// the metadata header exists (lazily writing it, plus any buffered
	// messages, on the first append to a fresh session). It does not
	// modify session.Messages in memory — callers should append to the
	// in-memory slice themselves before calling this.
	AppendMessage(session *Session, msg Message) error
	List() []SessionInfo
	Invalidate(key string)
}

// fileSessionStore implements SessionStore with JSONL persistence
type fileSessionStore struct {
	sessionsDir       string
	legacySessionsDir string
	sessions          map[string]*Session
	mu                sync.RWMutex
}

// NewFileSessionStore creates a new file-based session store
func NewFileSessionStore(sessionsDir string) (SessionStore, error) {
	return NewFileSessionStoreWithLegacy(sessionsDir, "")
}

// NewFileSessionStoreWithLegacy creates a session store with legacy migration support
func NewFileSessionStoreWithLegacy(sessionsDir, legacySessionsDir string) (SessionStore, error) {
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions dir: %w", err)
	}
	return &fileSessionStore{
		sessionsDir:       sessionsDir,
		legacySessionsDir: legacySessionsDir,
		sessions:          make(map[string]*Session),
	}, nil
}

func (s *fileSessionStore) sessionFile(key string) string {
	// Escape key for filesystem safety
	safeKey := filepath.Base(key)
	return filepath.Join(s.sessionsDir, safeKey+".jsonl")
}

func (s *fileSessionStore) GetOrCreate(key string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	var session *Session
	if existing, ok := s.sessions[key]; ok {
		return existing
	}

	// Try to load from disk
	session = s.loadSession(key)
	if session == nil {
		session = &Session{
			Key:       key,
			Messages:  []Message{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Metadata:  make(map[string]any),
		}
	}
	s.sessions[key] = session
	return session
}

func (s *fileSessionStore) loadSession(key string) *Session {
	filePath := s.sessionFile(key)

	// Check if file exists, if not try legacy path
	if _, err := os.Stat(filePath); os.IsNotExist(err) && s.legacySessionsDir != "" {
		legacyPath := s.legacySessionFile(key)
		if _, err := os.Stat(legacyPath); err == nil {
			// Migrate legacy session
			if err := s.migrateLegacySession(legacyPath, filePath); err != nil {
				slog.Warn("failed to migrate legacy session", "key", key, "error", err)
			} else {
				slog.Info("migrated session from legacy path", "key", key)
			}
		}
	}

	session, err := readSessionFile(filePath)
	if err != nil {
		return nil
	}
	return session
}

func readSessionFile(filePath string) (*Session, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var session Session
	session.Metadata = make(map[string]any)

	for scanner.Scan() {
		var data map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &data); err != nil {
			slog.Warn("failed to unmarshal session line", "error", err)
			continue
		}
		if data["_type"] == "metadata" {
			if v, ok := data["created_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					session.CreatedAt = t
				}
			}
			if v, ok := data["updated_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					session.UpdatedAt = t
				}
			}
			if v, ok := data["key"].(string); ok {
				session.Key = v
			}
			if v, ok := data["last_consolidated"].(float64); ok {
				session.LastConsolidated = int(v)
			}
			if v, ok := data["metadata"].(map[string]any); ok {
				session.Metadata = v
			}
		} else {
			var msg Message
			if content, ok := data["content"].(string); ok {
				msg.Content = content
			} else if contentArr, ok := data["content"].([]any); ok {
				msg.Content = contentArr
			}
			if role, ok := data["role"].(string); ok {
				msg.Role = role
			}
			if v, ok := data["tool_call_id"].(string); ok {
				msg.ToolCallID = v
			}
			if v, ok := data["name"].(string); ok {
				msg.Name = v
			}
			if v, ok := data["stop_reason"].(string); ok {
				msg.StopReason = v
			}
			if toolCallsRaw, ok := data["tool_calls"].([]any); ok {
				for _, tcRaw := range toolCallsRaw {
					if tcMap, ok := tcRaw.(map[string]any); ok {
						tc := ToolCall{}
						if id, ok := tcMap["id"].(string); ok {
							tc.ID = id
						}
						if name, ok := tcMap["name"].(string); ok {
							tc.Name = name
						}
						if args, ok := tcMap["arguments"].(map[string]any); ok {
							tc.Arguments = args
						}
						msg.ToolCalls = append(msg.ToolCalls, tc)
					}
				}
			}
			session.Messages = append(session.Messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &session, nil
}

// legacySessionFile returns the legacy session file path for a key
func (s *fileSessionStore) legacySessionFile(key string) string {
	safeKey := filepath.Base(key)
	return filepath.Join(s.legacySessionsDir, safeKey+".jsonl")
}

// migrateLegacySession moves a legacy session file to the new location
func (s *fileSessionStore) migrateLegacySession(legacyPath, newPath string) error {
	// Ensure the new directory exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("failed to create sessions dir: %w", err)
	}
	// Read legacy file content
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return fmt.Errorf("failed to read legacy file: %w", err)
	}
	// Write to new location
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write new session file: %w", err)
	}
	// Remove legacy file
	if err := os.Remove(legacyPath); err != nil {
		slog.Warn("failed to remove legacy session file", "path", legacyPath, "error", err)
	}
	return nil
}

func (s *fileSessionStore) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(session)
}

// saveLocked rewrites the entire session file. Caller must hold s.mu.
func (s *fileSessionStore) saveLocked(session *Session) error {
	session.UpdatedAt = time.Now()
	filePath := s.sessionFile(session.Key)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create session file: %w", err)
	}
	defer file.Close()

	if err := writeMetadataLine(file, session); err != nil {
		return err
	}

	// Write messages
	for _, msg := range session.Messages {
		if err := writeMessageLine(file, msg); err != nil {
			return err
		}
	}
	return nil
}

// AppendMessage writes one message entry to the end of the session file. On
// the very first append to a fresh session the metadata header + any already-
// in-memory messages are flushed first, matching pi-mono's "buffer until the
// first assistant arrives" semantics.
func (s *fileSessionStore) AppendMessage(session *Session, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.UpdatedAt = time.Now()
	filePath := s.sessionFile(session.Key)

	// If the file doesn't exist yet, fall through to a full rewrite. This
	// handles the first write of a new session (ensuring header + any
	// queued messages are on disk atomically) as well as recovery from a
	// file that was externally deleted. Note: session.Messages is expected
	// to already include `msg` — callers append to the in-memory slice
	// before calling AppendMessage, so saveLocked writes the complete
	// current state and is consistent with the append contract.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return s.saveLocked(session)
	} else if err != nil {
		return fmt.Errorf("stat session file: %w", err)
	}

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session file for append: %w", err)
	}
	defer file.Close()

	return writeMessageLine(file, msg)
}

func writeMetadataLine(w io.Writer, session *Session) error {
	meta := map[string]any{
		"_type":             "metadata",
		"key":               session.Key,
		"created_at":        session.CreatedAt.Format(time.RFC3339),
		"updated_at":        session.UpdatedAt.Format(time.RFC3339),
		"last_consolidated": session.LastConsolidated,
		"metadata":          session.Metadata,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if _, err := w.Write(metaBytes); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write metadata newline: %w", err)
	}
	return nil
}

func writeMessageLine(w io.Writer, msg Message) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	if _, err := w.Write(msgBytes); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write message newline: %w", err)
	}
	return nil
}

func (s *fileSessionStore) List() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infosByKey := map[string]SessionInfo{}
	paths, err := filepath.Glob(filepath.Join(s.sessionsDir, "*.jsonl"))
	if err == nil {
		for _, path := range paths {
			session, err := readSessionFile(path)
			if err != nil {
				slog.Warn("failed to read session for listing", "path", path, "error", err)
				continue
			}
			info := sessionInfo(session, sessionKeyFromPath(path))
			infosByKey[info.Key] = info
		}
	}

	for key, session := range s.sessions {
		infosByKey[key] = sessionInfo(session, key)
	}
	infos := make([]SessionInfo, 0, len(infosByKey))
	for _, info := range infosByKey {
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		if !infos[i].UpdatedAt.Equal(infos[j].UpdatedAt) {
			return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
		}
		return infos[i].Key < infos[j].Key
	})
	return infos
}

func sessionInfo(session *Session, fallbackKey string) SessionInfo {
	if session == nil {
		return SessionInfo{Key: fallbackKey}
	}
	key := session.Key
	if key == "" {
		key = fallbackKey
	}
	createdAt := session.CreatedAt
	updatedAt := session.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	return SessionInfo{
		Key:                key,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		MessageCount:       len(session.Messages),
		LastMessagePreview: lastUserMessagePreview(session.Messages),
	}
}

func sessionKeyFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func lastUserMessagePreview(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return compactPreview(contentText(messages[i].Content), 120)
	}
	return ""
}

func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch item := item.(type) {
			case string:
				parts = append(parts, item)
			case map[string]any:
				if text, ok := item["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func compactPreview(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func (s *fileSessionStore) Invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}
