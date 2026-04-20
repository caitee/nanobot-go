package session

import (
	"context"
	"strings"
)

// SearchResult represents a search result with relevance score.
type SearchResult struct {
	MessageIndex int
	Score       float64
	Message     Message
}

// VectorStore defines the interface for semantic vector storage.
// Implementations can use LanceDB, Milvus, Qdrant, or in-memory fallback.
type VectorStore interface {
	// Upsert adds or updates message embeddings for a session.
	Upsert(ctx context.Context, sessionKey string, messages []Message) error

	// Search finds the most similar messages to the query embedding.
	Search(ctx context.Context, sessionKey string, query string, topK int) ([]SearchResult, error)

	// Delete removes all embeddings for a session.
	Delete(ctx context.Context, sessionKey string) error
}

// InMemoryVectorStore is a simple in-memory fallback for VectorStore.
// Not suitable for production use with large message histories.
type InMemoryVectorStore struct {
	// embeddings maps sessionKey -> slice of message embeddings
	// Each embedding is a simple hash-based representation
	// For production, use a proper vector database like LanceDB
	embeddings map[string][]messageEmbedding
}

type messageEmbedding struct {
	index    int
	hash     uint64
	content  string
}

// NewInMemoryVectorStore creates a simple in-memory vector store.
func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		embeddings: make(map[string][]messageEmbedding),
	}
}

// simpleHash computes a simple hash for content matching.
func simpleHash(s string) uint64 {
	var h uint64
	for i := 0; i < len(s); i++ {
		h = h*31 + uint64(s[i])
	}
	return h
}

// Upsert adds or updates message embeddings for a session.
func (vs *InMemoryVectorStore) Upsert(ctx context.Context, sessionKey string, messages []Message) error {
	embs := make([]messageEmbedding, len(messages))
	for i, msg := range messages {
		content := extractMessageContent(msg.Content)
		embs[i] = messageEmbedding{
			index:   i,
			hash:    simpleHash(content),
			content: content,
		}
	}
	vs.embeddings[sessionKey] = embs
	return nil
}

// Search finds messages matching the query using simple keyword matching.
// This is a basic fallback - production should use proper embeddings.
func (vs *InMemoryVectorStore) Search(ctx context.Context, sessionKey string, query string, topK int) ([]SearchResult, error) {
	embs, ok := vs.embeddings[sessionKey]
	if !ok {
		return nil, nil
	}

	if topK <= 0 {
		topK = 5
	}
	if topK > len(embs) {
		topK = len(embs)
	}

	// Simple keyword matching score
	queryWords := splitWords(query)
	var scored []SearchResult
	for _, emb := range embs {
		score := keywordMatchScore(emb.content, queryWords)
		if score > 0 {
			scored = append(scored, SearchResult{
				MessageIndex: emb.index,
				Score:        score,
				Message:      Message{Content: emb.content},
			})
		}
	}

	// Sort by score descending
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Score > scored[i].Score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored, nil
}

// Delete removes all embeddings for a session.
func (vs *InMemoryVectorStore) Delete(ctx context.Context, sessionKey string) error {
	delete(vs.embeddings, sessionKey)
	return nil
}

func splitWords(s string) []string {
	var words []string
	var current []rune
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			if len(current) > 0 {
				words = append(words, string(current))
				current = nil
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

func keywordMatchScore(content string, queryWords []string) float64 {
	if len(queryWords) == 0 {
		return 0
	}
	contentLower := strings.ToLower(content)
	matches := 0
	for _, word := range queryWords {
		if strings.Contains(contentLower, strings.ToLower(word)) {
			matches++
		}
	}
	return float64(matches) / float64(len(queryWords))
}

// extractMessageContent extracts string content from a message Content field.
// Content can be a string, []any (content blocks), or other types.
func extractMessageContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	// Handle []any (content blocks)
	if blocks, ok := content.([]any); ok {
		var sb strings.Builder
		for _, block := range blocks {
			if m, ok := block.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
		return sb.String()
	}
	// Fallback
	return ""
}
