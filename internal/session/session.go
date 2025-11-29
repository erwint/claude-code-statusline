package session

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
)

// ReadInput reads session data from stdin if available
func ReadInput() *types.SessionInput {
	// Check if stdin has data available (non-blocking)
	stat, err := os.Stdin.Stat()
	if err != nil {
		config.DebugLog("stdin stat error: %v", err)
		return nil
	}

	config.DebugLog("stdin mode: %v, size: %d", stat.Mode(), stat.Size())

	// Check if it's a terminal (no piped input)
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		config.DebugLog("stdin is terminal, skipping")
		return nil
	}

	// Read all available data with a timeout
	resultCh := make(chan []byte, 1)
	go func() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			config.DebugLog("stdin read error: %v", err)
			resultCh <- nil
			return
		}
		resultCh <- data
	}()

	// Wait max 100ms for stdin data
	var data []byte
	select {
	case data = <-resultCh:
		config.DebugLog("stdin data received: %d bytes", len(data))
	case <-time.After(100 * time.Millisecond):
		config.DebugLog("stdin timeout")
		return nil
	}

	if len(data) == 0 {
		return nil
	}

	config.DebugLog("stdin content: %s", string(data))

	var session types.SessionInput
	if err := json.Unmarshal(data, &session); err != nil {
		config.DebugLog("json unmarshal error: %v", err)
		return nil
	}

	if session.Model != nil {
		config.DebugLog("parsed session: model=%s", session.Model.ID)
	}
	return &session
}
