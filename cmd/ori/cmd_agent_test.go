package main

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestGenerateSessionKey verifies that generateSessionKey produces unique
// session keys with the expected format.
func TestGenerateSessionKey(t *testing.T) {
	key1 := generateSessionKey()
	time.Sleep(2 * time.Millisecond)
	key2 := generateSessionKey()

	if key1 == key2 {
		t.Fatalf("expected unique session keys, got duplicate: %q", key1)
	}
	if !strings.HasPrefix(key1, "cli:session-") {
		t.Fatalf("expected session key to start with 'cli:session-', got %q", key1)
	}
	if !strings.HasPrefix(key2, "cli:session-") {
		t.Fatalf("expected session key to start with 'cli:session-', got %q", key2)
	}
}

// TestResolveSessionKey verifies the logic that decides whether to use
// the flag value or generate a new session key.
func TestResolveSessionKey(t *testing.T) {
	tests := []struct {
		name     string
		flagVal  string
		wantType string // "generated" or "explicit"
	}{
		{
			name:     "empty flag value generates new session",
			flagVal:  "",
			wantType: "generated",
		},
		{
			name:     "explicit user value is preserved",
			flagVal:  "my-custom-session",
			wantType: "explicit",
		},
		{
			name:     "explicit cli:direct is preserved if user really wants it",
			flagVal:  "cli:direct",
			wantType: "explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveSessionKey(tt.flagVal)
			switch tt.wantType {
			case "generated":
				if !strings.HasPrefix(result, "cli:session-") {
					t.Fatalf("expected generated session key (cli:session-*), got %q", result)
				}
			case "explicit":
				if result != tt.flagVal {
					t.Fatalf("expected explicit value %q, got %q", tt.flagVal, result)
				}
			}
		})
	}
}

func TestInteractiveProgramOptionsDoNotEnableMouseReporting(t *testing.T) {
	mouseCellMotion := teaStartupOptionsForTest(t, tea.WithMouseCellMotion())
	mouseAllMotion := teaStartupOptionsForTest(t, tea.WithMouseAllMotion())
	got := teaStartupOptionsForTest(t, interactiveProgramOptions()...)

	if got&mouseCellMotion != 0 {
		t.Fatal("interactive mode should not enable mouse cell-motion reporting; it prevents native terminal text selection")
	}
	if got&mouseAllMotion != 0 {
		t.Fatal("interactive mode should not enable mouse all-motion reporting; it prevents native terminal text selection")
	}
}

func TestInteractiveLogWriterDefaultsToDiscard(t *testing.T) {
	if got := interactiveLogWriter(false); got != io.Discard {
		t.Fatalf("interactive logs should be discarded unless --logs is set, got %T", got)
	}
	if got := interactiveLogWriter(true); got == io.Discard {
		t.Fatal("interactive logs should be visible when --logs is set")
	}
}

func TestConfigureInteractiveLoggingSuppressesDefaultLogs(t *testing.T) {
	previousSlog := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousSlog)
		log.SetOutput(os.Stderr)
	})

	var base bytes.Buffer
	log.SetOutput(&base)
	slog.SetDefault(slog.New(slog.NewTextHandler(&base, &slog.HandlerOptions{Level: slog.LevelWarn})))

	restore := configureInteractiveLogging(false)
	log.Print("hidden stdlib")
	slog.Warn("hidden slog")
	restore()

	if base.String() != "" {
		t.Fatalf("interactive logging should suppress default log output, got %q", base.String())
	}
}

func teaStartupOptionsForTest(t *testing.T, opts ...tea.ProgramOption) int64 {
	t.Helper()

	p := tea.NewProgram(nil, opts...)
	startupOptions := reflect.ValueOf(p).Elem().FieldByName("startupOptions")
	if !startupOptions.IsValid() {
		t.Fatal("bubbletea Program no longer exposes startupOptions; update this regression test")
	}
	return startupOptions.Int()
}
