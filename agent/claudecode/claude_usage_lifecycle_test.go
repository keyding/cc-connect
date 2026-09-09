//go:build !windows

package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestUsageProbeReturnsWhenTerminalClosesBeforeProcess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexec 0<&- 1>&- 2>&-\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := (&Agent{}).runClaudeUsageProbe(ctx); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed terminal must report an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("/usage remains blocked after terminal closed and context expired")
	}
}

func TestUsageProbeUsesConfiguredCLI(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	cli := filepath.Join(dir, "custom-claude")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = (&Agent{cmd: cli}).GetUsage(ctx)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("/usage ignored the configured CLI")
	}
}

func TestUsageProbeUsesIsolatedAccountAndFiltersSecrets(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "isolated")
	script := `#!/bin/sh
[ "$1" = "-n" ] && [ "$2" = "-iu" ] && [ "$3" = "usage-test-account" ] || exit 91
shift 3
case "$1" in --preserve-env=*) shift;; esac
[ "$1" = "--" ] || exit 92
shift
[ "$1" = "/usr/bin/true" ] && exit 0
[ "$1" = "sudo" ] && exit 1
[ "$1" = "/configured/claude" ] || exit 93
[ -z "$TELEGRAM_BOT_TOKEN" ] || exit 94
touch '` + marker + `'
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TELEGRAM_BOT_TOKEN", "must-not-reach-agent")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = (&Agent{cmd: "/configured/claude", spawnOpts: core.SpawnOptions{RunAsUser: "usage-test-account"}}).GetUsage(ctx)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("/usage failed to use the isolated account with filtered environment")
	}
}
