package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionStore defines session persistence interface
type SessionStore interface {
	GetOrCreate(key string) *Session
	Save(session *Session) error
	List() []SessionInfo
	Invalidate(key string)
}

// fileSessionStore implements SessionStore with JSONL persistence
type fileSessionStore struct {
	sessionsDir      string
	legacySessionsDir string
	sessions         map[string]*Session
	mu               sync.RWMutex
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
		sessionsDir:      sessionsDir,
		legacySessionsDir: legacySessionsDir,
		sessions:         make(map[string]*Session),
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

	file, err := os.Open(filePath)
	if err != nil {
		return nil
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
						msg.ToolCalls = append(msg.ToolCalls, tc)
					}
				}
			}
			session.Messages = append(session.Messages, msg)
		}
	}

	return &session
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

	session.UpdatedAt = time.Now()
	filePath := s.sessionFile(session.Key)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create session file: %w", err)
	}
	defer file.Close()

	// Write metadata
	meta := map[string]any{
		"_type":             "metadata",
		"key":               session.Key,
		"created_at":        session.CreatedAt.Format(time.RFC3339),
		"updated_at":        session.UpdatedAt.Format(time.RFC3339),
		"last_consolidated":  session.LastConsolidated,
		"metadata":          session.Metadata,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if _, err := file.Write(metaBytes); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Write messages
	for _, msg := range session.Messages {
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
		if _, err := file.Write(msgBytes); err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}
		if _, err := file.Write([]byte("\n")); err != nil {
			return fmt.Errorf("failed to write newline: %w", err)
		}
	}

	return nil
}

func (s *fileSessionStore) List() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var infos []SessionInfo
	for key, session := range s.sessions {
		infos = append(infos, SessionInfo{
			Key:          key,
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
			MessageCount: len(session.Messages),
		})
	}
	return infos
}

func (s *fileSessionStore) Invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}
