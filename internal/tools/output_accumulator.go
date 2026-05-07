package tools

import (
	"bytes"
	"fmt"
	"os"
	"sync"
)

// Default truncation limits for streaming command output.
const (
	DefaultMaxOutputLines = 2000
	DefaultMaxOutputBytes = 256 * 1024 // 256 KB
)

// TruncationInfo describes how the accumulator truncated output.
type TruncationInfo struct {
	Truncated    bool
	TruncatedBy  string // "lines" or "bytes"
	TotalLines   int
	OutputLines  int
	OutputBytes  int
	MaxLines     int
	MaxBytes     int
}

// OutputSnapshot is the public view of the accumulator at a moment in time.
type OutputSnapshot struct {
	Content        string
	Truncation     TruncationInfo
	FullOutputPath string // non-empty only when truncated and persisted
}

// OutputAccumulator buffers streaming output, applies line/byte limits, and
// persists the full stream to a temp file when truncation kicks in.
type OutputAccumulator struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	totalBytes int
	totalLines int
	maxLines  int
	maxBytes  int
	tempPath  string
	tempFile  *os.File
	truncated bool
	truncBy   string
	prefix    string
}

// NewOutputAccumulator returns an accumulator with default limits.
func NewOutputAccumulator(tempPrefix string) *OutputAccumulator {
	return &OutputAccumulator{
		maxLines: DefaultMaxOutputLines,
		maxBytes: DefaultMaxOutputBytes,
		prefix:   tempPrefix,
	}
}

// Append adds a chunk to the buffer, updating counters and spilling to the
// temp file once limits are hit.
func (a *OutputAccumulator) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalBytes += len(data)
	for _, b := range data {
		if b == '\n' {
			a.totalLines++
		}
	}

	// Always mirror to temp file once we've opened one; otherwise defer.
	if !a.truncated {
		a.buf.Write(data)
		if a.buf.Len() > a.maxBytes {
			a.markTruncated("bytes")
		} else if a.totalLines > a.maxLines {
			a.markTruncated("lines")
		}
		return
	}

	a.writeTemp(data)
}

// markTruncated opens the temp file and flushes the current buffer into it.
// Caller must hold a.mu.
func (a *OutputAccumulator) markTruncated(reason string) {
	a.truncated = true
	a.truncBy = reason
	f, err := os.CreateTemp("", a.prefix+"-*.log")
	if err != nil {
		// Temp file is a best-effort convenience — if it fails, we just
		// keep truncating in memory without a persisted copy.
		return
	}
	a.tempFile = f
	a.tempPath = f.Name()
	_, _ = a.tempFile.Write(a.buf.Bytes())
}

// writeTemp writes to the temp file if it's open. Caller must hold a.mu.
func (a *OutputAccumulator) writeTemp(data []byte) {
	if a.tempFile == nil {
		return
	}
	_, _ = a.tempFile.Write(data)
}

// Snapshot returns a consistent view of the accumulator.
func (a *OutputAccumulator) Snapshot() OutputSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	content := a.buf.String()
	if a.truncated {
		// Keep only the tail of the buffer so the model sees the most recent
		// and usually most relevant output.
		content = tailLines(content, a.maxLines)
		if len(content) > a.maxBytes {
			content = content[len(content)-a.maxBytes:]
		}
	}
	return OutputSnapshot{
		Content: content,
		Truncation: TruncationInfo{
			Truncated:   a.truncated,
			TruncatedBy: a.truncBy,
			TotalLines:  a.totalLines,
			OutputLines: countLines(content),
			OutputBytes: len(content),
			MaxLines:    a.maxLines,
			MaxBytes:    a.maxBytes,
		},
		FullOutputPath: a.tempPath,
	}
}

// Close closes the temp file if open.
func (a *OutputAccumulator) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tempFile != nil {
		err := a.tempFile.Close()
		a.tempFile = nil
		return err
	}
	return nil
}

// FormatTruncationNote appends a human-readable note about truncation.
func FormatTruncationNote(snap OutputSnapshot) string {
	t := snap.Truncation
	if !t.Truncated {
		return ""
	}
	if t.TruncatedBy == "bytes" {
		return fmt.Sprintf("\n\n[Output truncated: showing last %d of %d bytes. Full output: %s]",
			t.OutputBytes, byteTotal(snap), snap.FullOutputPath)
	}
	return fmt.Sprintf("\n\n[Output truncated: showing last %d of %d lines. Full output: %s]",
		t.OutputLines, t.TotalLines, snap.FullOutputPath)
}

func byteTotal(snap OutputSnapshot) int {
	// For the byte-truncated case we don't track totalBytes separately from
	// outputBytes; report what we have.
	if snap.Truncation.OutputBytes > 0 {
		return snap.Truncation.OutputBytes
	}
	return snap.Truncation.MaxBytes
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, b := range []byte(s) {
		if b == '\n' {
			n++
		}
	}
	if len(s) > 0 && s[len(s)-1] != '\n' {
		n++
	}
	return n
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			count++
			if count > n {
				return s[i+1:]
			}
		}
	}
	return s
}
