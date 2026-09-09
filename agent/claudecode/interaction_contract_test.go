package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// Exercises the actual local process and stream-json adapter. The CLI is a
// protocol fixture, not a real Claude model or an external permission service.
func TestClaudeInteraction_ProcessProtocolContracts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX CLI fixture")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	questionInput := map[string]any{"questions": []any{map[string]any{
		"question": "Which database?", "header": "Database", "multiSelect": false,
		"options": []any{map[string]any{"label": "SQLite", "description": "Embedded"}, map[string]any{"label": "Postgres", "description": "Server"}},
	}}}
	answeredInput := map[string]any{"questions": questionInput["questions"], "answers": map[string]any{"Which database?": "SQLite"}}
	for _, tc := range []struct {
		name, tool string
		mode       string
		input      map[string]any
		answer     core.PermissionResult
	}{
		{"allow", "Bash", "default", map[string]any{"command": "pwd"}, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{"command": "pwd"}}},
		{"deny", "Bash", "default", map[string]any{"command": "pwd"}, core.PermissionResult{Behavior: "deny", Message: "Declined by the initiator"}},
		{"question", "AskUserQuestion", "default", questionInput, core.PermissionResult{Behavior: "allow", UpdatedInput: answeredInput}},
		{"question-yolo", "AskUserQuestion", "yolo", questionInput, core.PermissionResult{Behavior: "allow", UpdatedInput: answeredInput}},
		{"question-dontAsk", "AskUserQuestion", "dontAsk", questionInput, core.PermissionResult{Behavior: "allow", UpdatedInput: answeredInput}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			requestID := "request-" + tc.name
			request, err := json.Marshal(map[string]any{"type": "control_request", "request_id": requestID, "request": map[string]any{"subtype": "can_use_tool", "tool_name": tc.tool, "input": tc.input}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "request.json"), append(request, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "claude-fixture")
			script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"interaction-history"}'
IFS= read -r user_message || exit 1
cat request.json
IFS= read -r control_response || exit 1
printf '%s\n' "$control_response" > response.json
printf '%s\n' '{"type":"result","subtype":"success","session_id":"interaction-history","result":"response recorded","is_error":false}'
while IFS= read -r rest; do :; done
`
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			agent, err := New(map[string]any{"cmd": bin, "work_dir": dir, "cc_data_dir": filepath.Join(dir, "cc-data"), "mode": tc.mode})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := agent.StartSession(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := session.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := session.Send("inspect project", "", nil, nil); err != nil {
				t.Fatal(err)
			}
			event := claudeInteractionEvent(t, ctx, session, core.EventPermissionRequest)
			if event.RequestID != requestID || event.ToolName != tc.tool || !reflect.DeepEqual(event.ToolInputRaw, tc.input) {
				t.Fatalf("permission event = %#v", event)
			}
			if tc.tool == "AskUserQuestion" {
				if len(event.Questions) != 1 || event.Questions[0].Question != "Which database?" || len(event.Questions[0].Options) != 2 || event.Questions[0].Options[0].Label != "SQLite" {
					t.Fatalf("question event = %#v", event.Questions)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "response.json")); !os.IsNotExist(err) {
				t.Fatalf("CLI received a response before caller answered: %v", err)
			}
			if err := session.RespondPermission(event.RequestID, tc.answer); err != nil {
				t.Fatal(err)
			}
			if event := claudeInteractionEvent(t, ctx, session, core.EventResult); !event.Done || event.Content != "response recorded" {
				t.Fatalf("result = %#v", event)
			}
			data, err := os.ReadFile(filepath.Join(dir, "response.json"))
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Type     string `json:"type"`
				Response struct {
					Subtype   string `json:"subtype"`
					RequestID string `json:"request_id"`
					Response  struct {
						Behavior     string         `json:"behavior"`
						Message      string         `json:"message"`
						UpdatedInput map[string]any `json:"updatedInput"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Type != "control_response" || wire.Response.Subtype != "success" || wire.Response.RequestID != requestID || wire.Response.Response.Behavior != tc.answer.Behavior {
				t.Fatalf("control response = %s", data)
			}
			if tc.answer.Behavior == "allow" && !reflect.DeepEqual(wire.Response.Response.UpdatedInput, tc.answer.UpdatedInput) {
				t.Fatalf("updated input = %#v, want %#v", wire.Response.Response.UpdatedInput, tc.answer.UpdatedInput)
			}
			if tc.answer.Behavior == "deny" && wire.Response.Response.Message != tc.answer.Message {
				t.Fatalf("denial = %s", data)
			}
		})
	}
}

func claudeInteractionEvent(t *testing.T, ctx context.Context, session core.AgentSession, kind core.EventType) core.Event {
	t.Helper()
	for {
		select {
		case event, ok := <-session.Events():
			if !ok {
				t.Fatal("session closed before expected event")
			}
			if event.Type == core.EventError {
				t.Fatalf("agent error: %v", event.Error)
			}
			if event.Type == kind {
				return event
			}
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", kind, ctx.Err())
		}
	}
}
