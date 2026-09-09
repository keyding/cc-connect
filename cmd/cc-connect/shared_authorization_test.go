package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	"github.com/chenhg5/cc-connect/platform/telegram"
)

type reloadAuthorizationPlatform struct {
	core.Platform
	replies []string
}

func (p *reloadAuthorizationPlatform) Reply(_ context.Context, _ any, text string) error {
	p.replies = append(p.replies, text)
	return nil
}
func (p *reloadAuthorizationPlatform) SetAllowFrom(value string) {
	p.Platform.(core.SharedAuthorizationUpdater).SetAllowFrom(value)
}
func (p *reloadAuthorizationPlatform) AuthorizeSharedControl(scope, user string) bool {
	return p.Platform.(core.SharedControlAuthorizer).AuthorizeSharedControl(scope, user)
}

func TestReloadConfig_UpdatesActualTelegramAuthorizationBeforeSharedOperations(t *testing.T) {
	platform, err := telegram.New(map[string]any{"token": "test-token", "allow_from": "alice,bob", "shared_session_directory": true})
	if err != nil {
		t.Fatal(err)
	}
	p := &reloadAuthorizationPlatform{Platform: platform}
	dir := t.TempDir()
	e := core.NewEngine("auth-project", &stubMainAgent{workDir: dir}, []core.Platform{p}, filepath.Join(dir, "state"), core.LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	send := func(user, command string) string {
		before := len(p.replies)
		e.ReceiveMessage(p, &core.Message{Platform: "telegram", SharedScope: "group", SessionKey: user, UserID: user, Content: command})
		return strings.Join(p.replies[before:], "\n")
	}
	send("alice", "/new Alpha")
	path := filepath.Join(dir, "config.toml")
	configText := fmt.Sprintf("[[projects]]\nname = 'auth-project'\n[projects.agent]\ntype = 'claudecode'\n[projects.agent.options]\nwork_dir = %q\n[[projects.platforms]]\ntype = 'telegram'\n[projects.platforms.options]\ntoken = 'test-token'\nallow_from = 'bob'\n", dir)
	if err := os.WriteFile(path, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reloadConfig(path, "auth-project", e); err != nil {
		t.Fatal(err)
	}
	if got := send("alice", "/list"); !strings.Contains(got, "Access denied") || strings.Contains(got, "Alpha") {
		t.Fatal(got)
	}
	if got := send("bob", "/list"); !strings.Contains(got, "Alpha") {
		t.Fatal(got)
	}
}
