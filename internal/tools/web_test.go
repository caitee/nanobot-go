package tools

import "testing"

func TestWebToolSearchResultCount(t *testing.T) {
	tests := []struct {
		name   string
		config *WebSearchConfig
		params map[string]any
		want   int
	}{
		{
			name:   "default count",
			params: map[string]any{},
			want:   5,
		},
		{
			name:   "configured default count",
			config: &WebSearchConfig{MaxResults: 7},
			params: map[string]any{},
			want:   7,
		},
		{
			name:   "explicit count wins",
			config: &WebSearchConfig{MaxResults: 7},
			params: map[string]any{"count": 3},
			want:   3,
		},
		{
			name:   "count is capped",
			config: &WebSearchConfig{MaxResults: 20},
			params: map[string]any{},
			want:   10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewWebToolWithConfig(tt.config)
			if got := tool.searchResultCount(tt.params); got != tt.want {
				t.Fatalf("searchResultCount() = %d; want %d", got, tt.want)
			}
		})
	}
}
