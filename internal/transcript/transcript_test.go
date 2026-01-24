package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erwint/claude-code-statusline/internal/types"
)

func TestParse_EmptyPath(t *testing.T) {
	result := Parse("")
	if result != nil {
		t.Error("expected nil for empty path")
	}
}

func TestParse_NonexistentFile(t *testing.T) {
	result := Parse("/nonexistent/path/transcript.jsonl")
	if result != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestParse_ToolTracking(t *testing.T) {
	// Create a temp transcript file
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"Read","input":{"file_path":"/path/to/file.go"}}]}}
{"timestamp":"2025-01-24T10:00:01Z","type":"result","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"file contents"}]}}
{"timestamp":"2025-01-24T10:00:02Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_2","name":"Edit","input":{"file_path":"/path/to/other.go"}}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// tool_1 should be completed, tool_2 should be running
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}

	completedCount := 0
	runningCount := 0
	for _, tool := range result.Tools {
		if tool.Status == "completed" {
			completedCount++
		}
		if tool.Status == "running" {
			runningCount++
		}
	}

	if completedCount != 1 {
		t.Errorf("expected 1 completed tool, got %d", completedCount)
	}
	if runningCount != 1 {
		t.Errorf("expected 1 running tool, got %d", runningCount)
	}
}

func TestParse_AgentTracking(t *testing.T) {
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"agent_1","name":"Task","input":{"subagent_type":"Explore","description":"searching files","model":"haiku"}}]}}
{"timestamp":"2025-01-24T10:00:05Z","type":"result","message":{"content":[{"type":"tool_result","tool_use_id":"agent_1","content":"found results"}]}}
{"timestamp":"2025-01-24T10:00:06Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"agent_2","name":"Task","input":{"subagent_type":"Plan","description":"designing implementation"}}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(result.Agents))
	}

	// Check agent_1 (completed)
	var agent1, agent2 *types.AgentEntry
	for i := range result.Agents {
		if result.Agents[i].ID == "agent_1" {
			agent1 = &result.Agents[i]
		}
		if result.Agents[i].ID == "agent_2" {
			agent2 = &result.Agents[i]
		}
	}

	if agent1 == nil || agent1.Status != "completed" {
		t.Error("agent_1 should be completed")
	}
	if agent1 != nil && agent1.Type != "Explore" {
		t.Errorf("agent_1 type should be Explore, got %s", agent1.Type)
	}

	if agent2 == nil || agent2.Status != "running" {
		t.Error("agent_2 should be running")
	}
}

func TestParse_TodoTracking(t *testing.T) {
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"todo_1","name":"TodoWrite","input":{"todos":[{"id":"1","subject":"Fix bug","status":"in_progress"},{"id":"2","subject":"Add tests","status":"pending"},{"id":"3","subject":"Setup","status":"completed"}]}}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Todos) != 3 {
		t.Errorf("expected 3 todos, got %d", len(result.Todos))
	}

	completed, total := GetTodoProgress(result)
	if completed != 1 || total != 3 {
		t.Errorf("expected 1/3 progress, got %d/%d", completed, total)
	}

	current := GetCurrentTodo(result)
	if current == nil || current.Subject != "Fix bug" {
		t.Error("expected current todo to be 'Fix bug'")
	}
}

func TestParse_SessionStart(t *testing.T) {
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"user","message":{"content":[{"type":"text","text":"hello"}]}}
{"timestamp":"2025-01-24T10:05:00Z","type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	expected, _ := time.Parse(time.RFC3339, "2025-01-24T10:00:00Z")
	if !result.SessionStart.Equal(expected) {
		t.Errorf("expected session start %v, got %v", expected, result.SessionStart)
	}
}

func TestParse_ToolError(t *testing.T) {
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"Bash","input":{"command":"invalid"}}]}}
{"timestamp":"2025-01-24T10:00:01Z","type":"result","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"error message","is_error":true}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(result.Tools))
	}

	if result.Tools[0].Status != "error" {
		t.Errorf("expected tool status 'error', got '%s'", result.Tools[0].Status)
	}
}

func TestGetRunningTools(t *testing.T) {
	data := &types.TranscriptData{
		Tools: []types.ToolEntry{
			{ID: "1", Name: "Read", Status: "completed"},
			{ID: "2", Name: "Edit", Status: "running"},
			{ID: "3", Name: "Bash", Status: "running"},
		},
	}

	running := GetRunningTools(data)
	if len(running) != 2 {
		t.Errorf("expected 2 running tools, got %d", len(running))
	}
}

func TestGetCompletedToolCounts(t *testing.T) {
	data := &types.TranscriptData{
		Tools: []types.ToolEntry{
			{Name: "Read", Status: "completed"},
			{Name: "Read", Status: "completed"},
			{Name: "Edit", Status: "completed"},
			{Name: "Bash", Status: "running"},
		},
	}

	counts := GetCompletedToolCounts(data)
	if counts["Read"] != 2 {
		t.Errorf("expected Read count 2, got %d", counts["Read"])
	}
	if counts["Edit"] != 1 {
		t.Errorf("expected Edit count 1, got %d", counts["Edit"])
	}
	if counts["Bash"] != 0 {
		t.Errorf("expected Bash count 0, got %d", counts["Bash"])
	}
}

func TestGetRunningAgents(t *testing.T) {
	data := &types.TranscriptData{
		Agents: []types.AgentEntry{
			{ID: "1", Type: "Explore", Status: "completed"},
			{ID: "2", Type: "Plan", Status: "running"},
		},
	}

	running := GetRunningAgents(data)
	if len(running) != 1 {
		t.Errorf("expected 1 running agent, got %d", len(running))
	}
	if running[0].Type != "Plan" {
		t.Errorf("expected running agent type 'Plan', got '%s'", running[0].Type)
	}
}

func TestTruncatePath(t *testing.T) {
	tests := []struct {
		path     string
		maxLen   int
		expected string
	}{
		{"/short/path.go", 20, "/short/path.go"},
		{"/very/long/path/to/file.go", 20, ".../file.go"},
		{"C:\\Windows\\path\\file.go", 20, ".../file.go"},
		{"/a/b/c/d/e/f/g.go", 15, ".../g.go"},
	}

	for _, tt := range tests {
		result := truncatePath(tt.path, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncatePath(%q, %d) = %q, expected %q",
				tt.path, tt.maxLen, result, tt.expected)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s        string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"longer string", 10, "longer ..."},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		result := truncate(tt.s, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, expected %q",
				tt.s, tt.maxLen, result, tt.expected)
		}
	}
}

func TestExtractTarget(t *testing.T) {
	tests := []struct {
		toolName string
		input    *ToolInput
		expected string
	}{
		{"Read", &ToolInput{FilePath: "/very/long/path/to/some/deeply/nested/file.go"}, ".../file.go"},
		{"Edit", &ToolInput{FilePath: "/short.go"}, "/short.go"},
		{"Glob", &ToolInput{Pattern: "**/*.go"}, "**/*.go"},
		{"Grep", &ToolInput{Pattern: "func main"}, "func main"},
		{"Bash", &ToolInput{Command: "go build"}, "go build"},
		{"WebFetch", &ToolInput{}, ""},
	}

	for _, tt := range tests {
		result := extractTarget(tt.toolName, tt.input)
		if result != tt.expected {
			t.Errorf("extractTarget(%q, ...) = %q, expected %q",
				tt.toolName, result, tt.expected)
		}
	}
}

func TestGetSessionDuration(t *testing.T) {
	tests := []struct {
		name      string
		data      *types.TranscriptData
		expected  string
		checkFunc func(string) bool
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: "",
		},
		{
			name:     "zero session start",
			data:     &types.TranscriptData{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSessionDuration(tt.data)
			if tt.checkFunc != nil {
				if !tt.checkFunc(result) {
					t.Errorf("GetSessionDuration() = %q, check failed", result)
				}
			} else if result != tt.expected {
				t.Errorf("GetSessionDuration() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestNilDataHelpers(t *testing.T) {
	// All helpers should handle nil gracefully
	if GetRunningTools(nil) != nil {
		t.Error("GetRunningTools(nil) should return nil")
	}
	if len(GetCompletedToolCounts(nil)) != 0 {
		t.Error("GetCompletedToolCounts(nil) should return empty map")
	}
	if GetRunningAgents(nil) != nil {
		t.Error("GetRunningAgents(nil) should return nil")
	}
	completed, total := GetTodoProgress(nil)
	if completed != 0 || total != 0 {
		t.Error("GetTodoProgress(nil) should return 0, 0")
	}
	if GetCurrentTodo(nil) != nil {
		t.Error("GetCurrentTodo(nil) should return nil")
	}
	if GetSessionDuration(nil) != "" {
		t.Error("GetSessionDuration(nil) should return empty string")
	}
}

func TestGetTodoProgress(t *testing.T) {
	tests := []struct {
		name              string
		todos             []types.TodoItem
		expectedCompleted int
		expectedTotal     int
	}{
		{
			name:              "empty todos",
			todos:             []types.TodoItem{},
			expectedCompleted: 0,
			expectedTotal:     0,
		},
		{
			name: "all pending",
			todos: []types.TodoItem{
				{Subject: "Task 1", Status: "pending"},
				{Subject: "Task 2", Status: "pending"},
			},
			expectedCompleted: 0,
			expectedTotal:     2,
		},
		{
			name: "all completed",
			todos: []types.TodoItem{
				{Subject: "Task 1", Status: "completed"},
				{Subject: "Task 2", Status: "completed"},
			},
			expectedCompleted: 2,
			expectedTotal:     2,
		},
		{
			name: "mixed status",
			todos: []types.TodoItem{
				{Subject: "Task 1", Status: "completed"},
				{Subject: "Task 2", Status: "in_progress"},
				{Subject: "Task 3", Status: "pending"},
			},
			expectedCompleted: 1,
			expectedTotal:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &types.TranscriptData{Todos: tt.todos}
			completed, total := GetTodoProgress(data)
			if completed != tt.expectedCompleted || total != tt.expectedTotal {
				t.Errorf("GetTodoProgress() = (%d, %d), want (%d, %d)",
					completed, total, tt.expectedCompleted, tt.expectedTotal)
			}
		})
	}
}

func TestGetCurrentTodo(t *testing.T) {
	tests := []struct {
		name     string
		todos    []types.TodoItem
		expected *types.TodoItem
	}{
		{
			name:     "empty todos",
			todos:    []types.TodoItem{},
			expected: nil,
		},
		{
			name: "no in_progress",
			todos: []types.TodoItem{
				{Subject: "Task 1", Status: "completed"},
				{Subject: "Task 2", Status: "pending"},
			},
			expected: nil,
		},
		{
			name: "has in_progress",
			todos: []types.TodoItem{
				{Subject: "Task 1", Status: "completed"},
				{Subject: "Current Task", Status: "in_progress"},
				{Subject: "Task 3", Status: "pending"},
			},
			expected: &types.TodoItem{Subject: "Current Task", Status: "in_progress"},
		},
		{
			name: "first in_progress returned",
			todos: []types.TodoItem{
				{Subject: "First", Status: "in_progress"},
				{Subject: "Second", Status: "in_progress"},
			},
			expected: &types.TodoItem{Subject: "First", Status: "in_progress"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &types.TranscriptData{Todos: tt.todos}
			result := GetCurrentTodo(data)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("GetCurrentTodo() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Errorf("GetCurrentTodo() = nil, want %v", tt.expected)
				} else if result.Subject != tt.expected.Subject || result.Status != tt.expected.Status {
					t.Errorf("GetCurrentTodo() = %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

func TestParse_MalformedJSON(t *testing.T) {
	content := `not json
{"timestamp":"2025-01-24T10:00:00Z","type":"user","message":{"content":[{"type":"text","text":"hello"}]}}
also not json
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not panic, should parse what it can
	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result even with malformed lines")
	}

	// Should still get session start from valid line
	if result.SessionStart.IsZero() {
		t.Error("should have parsed session start from valid line")
	}
}

func TestParse_TodoOverwrite(t *testing.T) {
	// Multiple TodoWrite calls should overwrite previous todos
	content := `{"timestamp":"2025-01-24T10:00:00Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"todo_1","name":"TodoWrite","input":{"todos":[{"id":"1","subject":"First","status":"pending"}]}}]}}
{"timestamp":"2025-01-24T10:00:01Z","type":"assistant","message":{"content":[{"type":"tool_use","id":"todo_2","name":"TodoWrite","input":{"todos":[{"id":"1","subject":"Updated","status":"completed"},{"id":"2","subject":"New","status":"pending"}]}}]}}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Parse(tmpFile)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have the second TodoWrite's todos
	if len(result.Todos) != 2 {
		t.Errorf("expected 2 todos, got %d", len(result.Todos))
	}

	if result.Todos[0].Subject != "Updated" {
		t.Errorf("expected first todo subject 'Updated', got '%s'", result.Todos[0].Subject)
	}
}
