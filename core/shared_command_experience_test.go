package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type sharedMenuPlatform struct{ queueTestPlatform }

func (*sharedMenuPlatform) SharedSessionDirectoryEnabled() bool { return true }

func TestSharedToolMessagesRespectDisplayConfig(t *testing.T) {
	for _, show := range []bool{false, true} {
		t.Run(fmt.Sprint(show), func(t *testing.T) {
			a := &queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}
			p := &queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
			e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangChinese)
			e.SetDisplayConfig(DisplayCfg{ToolMessages: show})
			t.Cleanup(func() { _ = e.Stop() })
			queueMessage(e, p, "alice", "1", "/new Alpha")
			queueMessage(e, p, "alice", "2", "hello")
			session := nextQueueSession(t, a)
			<-session.sent
			session.events <- Event{Type: EventToolUse, ToolName: "WebSearch"}
			session.events <- Event{Type: EventResult, Done: true, Content: "answer"}
			waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "answer") })
			if got := strings.Join(p.getSent(), "\n"); strings.Contains(got, "WebSearch") != show {
				t.Fatalf("tool display configuration ignored: %s", got)
			}
		})
	}
}

func TestSharedCommandMenuIncludesControlsAndDescriptions(t *testing.T) {
	p := &sharedMenuPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}
	e := NewEngine("project", &queueTestAgent{dir: t.TempDir()}, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangChinese)
	t.Cleanup(func() { _ = e.Stop() })
	commands, _ := e.menuCommandsForPlatform(p.Name())
	got := map[string]string{}
	for _, c := range commands {
		got[c.Command] = c.Description
	}
	for _, name := range []string{"history", "queue", "cancel", "stop", "resume", "resolve", "continue", "delete", "approve", "deny", "answer", "help"} {
		if got[name] == "" || got[name] == name {
			t.Errorf("missing command description: %s", name)
		}
	}
}

func TestSharedCommandMenuHelpAndDisabledCommandsAgree(t *testing.T) {
	p := &sharedMenuPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}
	for _, lang := range []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish} {
		e := NewEngine("project", &queueTestAgent{dir: t.TempDir()}, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), lang)
		e.SetDisabledCommands([]string{"delete"})
		menu, _ := e.menuCommandsForPlatform(p.Name())
		e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: "help", Content: "/help", ReplyCtx: "alice"})
		sent := p.getSent()
		help := sent[len(sent)-1]
		for _, c := range menu {
			if c.Description == "" || strings.HasPrefix(c.Description, "shared_cmd_") || !strings.Contains(help, "/"+c.Command+" — "+c.Description) {
				t.Fatalf("missing %s description in %s: %s", c.Command, lang, help)
			}
		}
		if strings.Contains(help, "/delete") || strings.Contains(help, "/model") {
			t.Fatal("unsupported or disabled command advertised")
		}
		e.SetDisabledCommands([]string{"*"})
		menu, _ = e.menuCommandsForPlatform(p.Name())
		if len(menu) != 0 {
			t.Fatal(menu)
		}
		_ = e.Stop()
	}
}

func TestSharedAcceptanceMessagesCanHideImmediateReceiptButKeepQueueNotice(t *testing.T) {
	a := &queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}
	p := &queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangChinese)
	show := false
	e.SetDisplayConfig(DisplayCfg{SharedAcceptanceMessages: &show})
	t.Cleanup(func() { _ = e.Stop() })
	queueMessage(e, p, "alice", "1", "/new Alpha")
	queueMessage(e, p, "alice", "2", "first")
	session := nextQueueSession(t, a)
	<-session.sent
	if got := strings.Join(p.getSent(), "\n"); strings.Contains(got, "已保存至会话") {
		t.Fatal(got)
	}
	queueMessage(e, p, "alice", "3", "second")
	sent := p.getSent()
	if !strings.Contains(sent[len(sent)-1], "前方有 1 个请求") {
		t.Fatal(sent)
	}
	session.events <- Event{Type: EventResult, Done: true, Content: "first done"}
	second := nextQueueSession(t, a)
	<-second.sent
	second.events <- Event{Type: EventResult, Done: true, Content: "second done"}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "second done") })
}
