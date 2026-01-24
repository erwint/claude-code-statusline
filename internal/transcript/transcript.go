package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
)

// Maximum entries to keep for display
const (
	MaxTools  = 20
	MaxAgents = 10
)

// TranscriptEntry represents a single entry in the transcript JSONL
type TranscriptEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"` // "assistant", "user", "result"
	Message   struct {
		Content []ContentBlock `json:"content"`
	} `json:"message"`
}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type        string          `json:"type"` // "tool_use", "tool_result", "text"
	ID          string          `json:"id"`   // tool_use_id
	ToolUseID   string          `json:"tool_use_id"` // for tool_result
	Name        string          `json:"name"`
	Input       json.RawMessage `json:"input"`
	Content     string          `json:"content"` // for tool_result
	IsError     bool            `json:"is_error"`
}

// ToolInput holds common tool input fields
type ToolInput struct {
	// For file operations (Read, Edit, Write, Glob, Grep)
	FilePath string `json:"file_path"`
	Path     string `json:"path"`
	Pattern  string `json:"pattern"`
	Command  string `json:"command"`

	// For Task (subagents)
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	Model        string `json:"model"`

	// For TodoWrite
	Todos []TodoInput `json:"todos"`
}

// TodoInput represents a todo item from TodoWrite
type TodoInput struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

// Parse reads the transcript file and extracts tools, agents, and todos
func Parse(transcriptPath string) *types.TranscriptData {
	if transcriptPath == "" {
		return nil
	}

	file, err := os.Open(transcriptPath)
	if err != nil {
		config.DebugLog("transcript: failed to open %s: %v", transcriptPath, err)
		return nil
	}
	defer file.Close()

	data := &types.TranscriptData{
		Tools:  make([]types.ToolEntry, 0),
		Agents: make([]types.AgentEntry, 0),
		Todos:  make([]types.TodoItem, 0),
	}

	// Maps for matching tool_use with tool_result
	pendingTools := make(map[string]*types.ToolEntry)
	pendingAgents := make(map[string]*types.AgentEntry)

	scanner := bufio.NewScanner(file)
	// Increase buffer size for potentially large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 5*1024*1024) // 5MB max line size

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry TranscriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			config.DebugLog("transcript: failed to parse line: %v", err)
			continue
		}

		// Track session start from first entry
		if data.SessionStart.IsZero() && entry.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
				data.SessionStart = ts
			}
		}

		processEntry(&entry, data, pendingTools, pendingAgents)
	}

	if err := scanner.Err(); err != nil {
		config.DebugLog("transcript: scanner error: %v", err)
	}

	// Add any remaining pending tools/agents as running
	for _, tool := range pendingTools {
		tool.Status = "running"
		data.Tools = append(data.Tools, *tool)
	}
	for _, agent := range pendingAgents {
		agent.Status = "running"
		data.Agents = append(data.Agents, *agent)
	}

	// Trim to max entries (keep most recent)
	if len(data.Tools) > MaxTools {
		data.Tools = data.Tools[len(data.Tools)-MaxTools:]
	}
	if len(data.Agents) > MaxAgents {
		data.Agents = data.Agents[len(data.Agents)-MaxAgents:]
	}

	return data
}

func processEntry(entry *TranscriptEntry, data *types.TranscriptData,
	pendingTools map[string]*types.ToolEntry, pendingAgents map[string]*types.AgentEntry) {

	ts, _ := time.Parse(time.RFC3339, entry.Timestamp)

	for _, block := range entry.Message.Content {
		switch block.Type {
		case "tool_use":
			processToolUse(&block, ts, data, pendingTools, pendingAgents)
		case "tool_result":
			processToolResult(&block, ts, data, pendingTools, pendingAgents)
		}
	}
}

func processToolUse(block *ContentBlock, ts time.Time, data *types.TranscriptData,
	pendingTools map[string]*types.ToolEntry, pendingAgents map[string]*types.AgentEntry) {

	var input ToolInput
	if err := json.Unmarshal(block.Input, &input); err != nil {
		config.DebugLog("transcript: failed to parse tool input: %v", err)
	}

	// Handle Task tool (subagents)
	if block.Name == "Task" {
		agent := &types.AgentEntry{
			ID:          block.ID,
			Type:        input.SubagentType,
			Description: truncate(input.Description, 50),
			Model:       input.Model,
			Status:      "running",
			StartTime:   ts,
		}
		if agent.Type == "" {
			agent.Type = "unknown"
		}
		pendingAgents[block.ID] = agent
		return
	}

	// Handle TodoWrite tool
	if block.Name == "TodoWrite" {
		data.Todos = make([]types.TodoItem, 0, len(input.Todos))
		for _, todo := range input.Todos {
			data.Todos = append(data.Todos, types.TodoItem{
				Subject: todo.Subject,
				Status:  todo.Status,
			})
		}
		return
	}

	// Handle regular tools
	tool := &types.ToolEntry{
		ID:        block.ID,
		Name:      block.Name,
		Target:    extractTarget(block.Name, &input),
		Status:    "running",
		StartTime: ts,
	}
	pendingTools[block.ID] = tool
}

func processToolResult(block *ContentBlock, ts time.Time, data *types.TranscriptData,
	pendingTools map[string]*types.ToolEntry, pendingAgents map[string]*types.AgentEntry) {

	// Check if it's an agent result
	if agent, ok := pendingAgents[block.ToolUseID]; ok {
		agent.Status = "completed"
		if block.IsError {
			agent.Status = "error"
		}
		agent.EndTime = ts
		data.Agents = append(data.Agents, *agent)
		delete(pendingAgents, block.ToolUseID)
		return
	}

	// Check if it's a tool result
	if tool, ok := pendingTools[block.ToolUseID]; ok {
		tool.Status = "completed"
		if block.IsError {
			tool.Status = "error"
		}
		tool.EndTime = ts
		data.Tools = append(data.Tools, *tool)
		delete(pendingTools, block.ToolUseID)
		return
	}
}

func extractTarget(toolName string, input *ToolInput) string {
	switch toolName {
	case "Read", "Edit", "Write", "NotebookEdit":
		if input.FilePath != "" {
			return truncatePath(input.FilePath, 30)
		}
	case "Glob", "Grep":
		if input.Pattern != "" {
			return truncate(input.Pattern, 30)
		}
		if input.Path != "" {
			return truncatePath(input.Path, 30)
		}
	case "Bash":
		if input.Command != "" {
			return truncate(input.Command, 30)
		}
	case "WebFetch", "WebSearch":
		// Could extract URL, but usually too long
		return ""
	}
	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func truncatePath(path string, maxLen int) string {
	// Normalize Windows paths
	path = strings.ReplaceAll(path, "\\", "/")

	if len(path) <= maxLen {
		return path
	}

	// Try to preserve filename
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		if len(filename) < maxLen-4 {
			return ".../" + filename
		}
	}

	return path[:maxLen-3] + "..."
}

// GetRunningTools returns only tools with status "running"
func GetRunningTools(data *types.TranscriptData) []types.ToolEntry {
	if data == nil {
		return nil
	}
	var running []types.ToolEntry
	for _, t := range data.Tools {
		if t.Status == "running" {
			running = append(running, t)
		}
	}
	return running
}

// GetCompletedToolCounts returns a map of tool names to completion counts
func GetCompletedToolCounts(data *types.TranscriptData) map[string]int {
	counts := make(map[string]int)
	if data == nil {
		return counts
	}
	for _, t := range data.Tools {
		if t.Status == "completed" || t.Status == "error" {
			counts[t.Name]++
		}
	}
	return counts
}

// GetRunningAgents returns only agents with status "running"
func GetRunningAgents(data *types.TranscriptData) []types.AgentEntry {
	if data == nil {
		return nil
	}
	var running []types.AgentEntry
	for _, a := range data.Agents {
		if a.Status == "running" {
			running = append(running, a)
		}
	}
	return running
}

// GetTodoProgress returns completed count and total count
func GetTodoProgress(data *types.TranscriptData) (completed, total int) {
	if data == nil {
		return 0, 0
	}
	total = len(data.Todos)
	for _, t := range data.Todos {
		if t.Status == "completed" {
			completed++
		}
	}
	return completed, total
}

// GetCurrentTodo returns the in-progress todo, if any
func GetCurrentTodo(data *types.TranscriptData) *types.TodoItem {
	if data == nil {
		return nil
	}
	for i := range data.Todos {
		if data.Todos[i].Status == "in_progress" {
			return &data.Todos[i]
		}
	}
	return nil
}

// GetSessionDuration returns the session duration as a formatted string
func GetSessionDuration(data *types.TranscriptData) string {
	if data == nil || data.SessionStart.IsZero() {
		return ""
	}

	duration := time.Since(data.SessionStart)
	mins := int(duration.Minutes())

	if mins < 1 {
		return "<1m"
	}
	if mins < 60 {
		return formatInt(mins) + "m"
	}

	hours := mins / 60
	remainingMins := mins % 60
	return formatInt(hours) + "h" + formatInt(remainingMins) + "m"
}

func formatInt(n int) string {
	return fmt.Sprintf("%d", n)
}
