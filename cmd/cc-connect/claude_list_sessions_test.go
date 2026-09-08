//go:build !no_claudecode

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestClaudeListSessionsHelperReadsTargetHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, ".claude", "projects", "-srv-test-project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	content := "{\"type\":\"user\",\"message\":{\"content\":\"existing conversation\"}}\n"
	if err := os.WriteFile(filepath.Join(project, "saved-session.jsonl"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "result")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	if code := runClaudeListSessions([]string{"/srv/test-project"}); code != 0 {
		t.Fatalf("helper exit %d", code)
	}
	if _, err := output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var sessions []core.AgentSessionInfo
	if err := json.NewDecoder(output).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "saved-session" || sessions[0].Summary != "existing conversation" {
		t.Fatalf("wrong session list: %+v", sessions)
	}
}
