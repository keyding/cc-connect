package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestPendingNamedSessionCommands(t *testing.T) {
	p := &pendingRoutePlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	e := NewEngine("test", &stubListAgent{}, []Platform{p}, "", LangEnglish)
	msg := &Message{SessionKey: "telegram:group:user1", ReplyCtx: "ctx"}
	e.cmdNew(p, msg, []string{"image"})
	for _, command := range []string{"list", "current"} {
		t.Run(command, func(t *testing.T) {
			p.sent = nil
			if command == "list" {
				e.cmdList(p, msg, nil)
			} else {
				e.cmdCurrent(p, msg)
			}
			if len(p.sent) != 1 || !strings.Contains(p.sent[0], "image") {
				t.Fatalf("named pending session missing: %q", p.sent)
			}
		})
	}
}

func TestPendingNamedSessionDoesNotChangeSwitchNumbers(t *testing.T) {
	p := &pendingRoutePlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	agent := &stubListAgent{sessions: []AgentSessionInfo{{ID: "existing", Summary: "previous", MessageCount: 2}}}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	msg := &Message{SessionKey: "telegram:group:user1", ReplyCtx: "ctx"}
	e.cmdNew(p, msg, []string{"image"})
	p.sent = nil
	e.cmdList(p, msg, nil)
	if len(p.sent) != 1 || !strings.Contains(p.sent[0], "image") || !strings.Contains(p.sent[0], "**1.** previous") {
		t.Fatalf("list: %q", p.sent)
	}
	p.sent = nil
	e.cmdList(p, &Message{SessionKey: "telegram:group:user2", ReplyCtx: "ctx"}, nil)
	if strings.Contains(p.sent[0], "image") {
		t.Fatal("another user's pending name leaked")
	}
	e.sessions.GetOrCreateActive(msg.SessionKey).SetAgentSessionID("existing", "claudecode")
	p.sent = nil
	e.cmdList(p, msg, nil)
	if strings.Contains(p.sent[0], "Current pending session") {
		t.Fatal("pending entry remains after start")
	}
}

func TestPendingSessionVisibleAfterTopicChangeAndSwitch(t *testing.T) {
	p := &pendingRoutePlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	e := NewEngine("test", &stubListAgent{sessions: []AgentSessionInfo{{ID: "old", Summary: "old"}}}, []Platform{p}, "", LangEnglish)
	original := "telegram:-100:123"
	topic := "telegram:-100:4:123"
	e.cmdNew(p, &Message{SessionKey: original, ReplyCtx: "ctx"}, []string{"image"})
	e.sessions.SwitchToAgentSession(original, "old", "claudecode", "old")
	e.sessions.SwitchToAgentSession(topic, "old", "claudecode", "old")
	p.sent = nil
	e.cmdList(p, &Message{SessionKey: topic, ReplyCtx: "ctx"}, nil)
	if len(p.sent) != 1 || !strings.Contains(p.sent[0], "image") {
		t.Fatalf("pending session hidden across topic after switch: %q", p.sent)
	}
}

func TestPendingSwitchPersistsAndRejectsOtherUsers(t *testing.T) {
	p := &pendingRoutePlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	e := NewEngine("test", &stubListAgent{}, []Platform{p}, "", LangEnglish)
	path := t.TempDir() + "/sessions.json"
	e.sessions = NewSessionManager(path)
	draft := e.sessions.NewSession("telegram:-100:123", "image")
	key := "telegram:-100:4:123"
	e.cmdSwitch(p, &Message{SessionKey: key, ReplyCtx: "ctx"}, []string{"pending:" + draft.ID})
	if e.sessions.ActiveSessionID(key) != draft.ID {
		t.Fatal("draft not activated")
	}
	loaded := NewSessionManager(path)
	if loaded.ActiveSessionID(key) != draft.ID {
		t.Fatal("draft binding not persisted")
	}
	if len(loaded.pendingSessionsForRoute(p, key)) != 1 {
		t.Fatal("duplicate or lost draft")
	}
	for _, other := range []string{"telegram:-100:4:456", "telegram:-200:4:123"} {
		if err := loaded.activatePendingSession(p, other, draft.ID); err == nil {
			t.Fatal("cross-user/chat access allowed")
		}
	}
	draft.SetAgentSessionID("started", "claudecode")
	if err := e.sessions.activatePendingSession(p, key, draft.ID); err == nil {
		t.Fatal("started session accepted as pending")
	}
}

func TestPendingNumberedListAndSwitch(t *testing.T) {
	for _, n := range []int{0, 1, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p := &pendingRoutePlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
			agent := &stubListAgent{}
			for i := 0; i < n; i++ {
				agent.sessions = append(agent.sessions, AgentSessionInfo{ID: fmt.Sprintf("history-%d", i), Summary: "history"})
			}
			e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
			key := "telegram:group:user1"
			draft := e.sessions.NewSession(key, "image")
			e.sessions.SwitchToAgentSession(key, "history-0", "claudecode", "history")
			msg := &Message{SessionKey: key, ReplyCtx: "ctx"}
			page := []string{}
			if n == 20 {
				page = []string{"2"}
			}
			e.cmdList(p, msg, page)
			if len(p.sent) != 1 || !strings.Contains(p.sent[0], fmt.Sprintf("**%d.** image (pending)", n+1)) {
				t.Fatalf("list: %q", p.sent)
			}
			e.cmdSwitch(p, msg, []string{fmt.Sprint(n + 1)})
			if e.sessions.ActiveSessionID(key) != draft.ID {
				t.Fatal("numeric switch selected wrong session")
			}
			if draft.GetAgentSessionID() != "" {
				t.Fatal("pending id passed as agent session id")
			}
		})
	}
}

type pendingRoutePlatform struct{ *stubPlatformEngine }

func (p *pendingRoutePlatform) PendingSessionRouteMatches(a, b string) bool {
	// The fake platform defines its own route policy; Telegram parsing is tested there.
	x, y := strings.Split(a, ":"), strings.Split(b, ":")
	return len(x) >= 3 && len(y) >= 3 && x[0] == y[0] && x[1] == y[1] && x[len(x)-1] == y[len(y)-1]
}
