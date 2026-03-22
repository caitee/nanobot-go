package session

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	sessionsDir string
	sessions    map[string]*Session
	mu          sync.RWMutex
}

// NewFileSessionStore creates a new file-based session store
func NewFileSessionStore(sessionsDir string) (SessionStore, error) {
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions dir: %w", err)
	}
	return &fileSessionStore{
		sessionsDir: sessionsDir,
		sessions:    make(map[string]*Session),
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
			session.Messages = append(session.Messages, msg)
		}
	}

	return &session
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
	metaBytes, _ := json.Marshal(meta)
	file.Write(metaBytes)
	file.Write([]byte("\n"))

	// Write messages
	for _, msg := range session.Messages {
		msgBytes, _ := json.Marshal(msg)
		file.Write(msgBytes)
		file.Write([]byte("\n"))
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
