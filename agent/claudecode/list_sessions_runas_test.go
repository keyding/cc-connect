//go:build !windows

package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestListSessionsRunAsUserDoesNotReadSupervisorHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TELEGRAM_BOT_TOKEN", "must-not-reach-helper")
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectKey(workDir))
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "supervisor-session.jsonl"), []byte(`{"type":"user","message":{"content":"wrong account"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
[ "$1" = "-n" ] && [ "$2" = "-iu" ] && [ "$3" = "isolated-user" ] && [ "$4" = "--" ] || exit 91
[ "$6" = "_claude-list-sessions" ] && [ "$#" = 7 ] || exit 92
[ -z "$TELEGRAM_BOT_TOKEN" ] || exit 93
printf '%s\n' '[{"ID":"target-session","Summary":"target user history","MessageCount":2}]'
`
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &Agent{workDir: workDir, spawnOpts: core.SpawnOptions{RunAsUser: "isolated-user"}}
	if !a.ValidateSessionID(context.Background(), "target-session") {
		t.Error("resume validation rejected the target account session")
	}
	if a.ValidateSessionID(context.Background(), "supervisor-session") {
		t.Error("resume validation accepted the supervisor account session")
	}
	got, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "target-session" || got[0].MessageCount != 2 {
		t.Fatalf("expected target account history, got %+v", got)
	}
}

func TestListSessionsRunAsUserFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &Agent{workDir: t.TempDir(), spawnOpts: core.SpawnOptions{RunAsUser: "isolated-user"}}
	if valid, err := a.CheckSessionID(context.Background(), "saved"); err == nil || valid {
		t.Fatal("validation failure must remain distinguishable from a missing transcript")
	}

	if _, err := a.ListSessions(context.Background()); err == nil {
		t.Fatal("helper failure must be reported, not an empty session list")
	}
}
