//go:build !windows

package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestDeleteSessionUsesRunAsUser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	t.Setenv("TELEGRAM_BOT_TOKEN", "not-for-agent")
	script := `#!/bin/sh
[ "$1" = "-n" ] && [ "$2" = "-iu" ] && [ "$3" = "isolated-user" ] || exit 91
[ "$6" = "_claude-delete-session" ] && [ "$8" = "target-session" ] && [ "$#" = 8 ] || exit 92
[ -z "$TELEGRAM_BOT_TOKEN" ] || exit 93
`
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &Agent{workDir: t.TempDir(), spawnOpts: core.SpawnOptions{RunAsUser: "isolated-user"}}
	if err := a.DeleteSession(context.Background(), "target-session"); err != nil {
		t.Fatal(err)
	}
}
func TestDeleteLocalSessionOnlyTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectKey(work))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"target", "keep"} {
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"../keep", "", "..", "a/b"} {
		if DeleteLocalSession(work, id) == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	if err := DeleteLocalSession(work, "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "target.jsonl")); !os.IsNotExist(err) {
		t.Fatal("target not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.jsonl")); err != nil {
		t.Fatal("other transcript changed")
	}
}

func TestDeleteSessionRunAsUserFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectKey(work))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &Agent{workDir: work, spawnOpts: core.SpawnOptions{RunAsUser: "isolated-user"}}
	if err := a.DeleteSession(context.Background(), "target"); err == nil {
		t.Fatal("helper failure was ignored")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("supervisor transcript changed: %v", err)
	}
}
