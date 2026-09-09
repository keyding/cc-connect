package core

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type interactionTestPlatform struct {
	linkTestPlatform
	authMu      sync.Mutex
	denied      map[string]bool
	buttonSets  [][][]ButtonOption
	updates     map[string]string
	failUpdates bool
}

func (p *interactionTestPlatform) AuthorizeSharedControl(scope, user string) bool {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	return !p.denied[user]
}
func (p *interactionTestPlatform) deny(user string) {
	p.authMu.Lock()
	if p.denied == nil {
		p.denied = map[string]bool{}
	}
	p.denied[user] = true
	p.authMu.Unlock()
}
func (p *interactionTestPlatform) SendWithButtonsWithReceipt(ctx context.Context, target any, text string, buttons [][]ButtonOption, record func(MessageReference) error) error {
	p.authMu.Lock()
	p.buttonSets = append(p.buttonSets, buttons)
	p.authMu.Unlock()
	return p.ReplyWithReceipt(ctx, target, text, record)
}

func (p *interactionTestPlatform) UpdateInteractionMessage(_ context.Context, ref MessageReference, text string) error {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	if p.updates == nil {
		p.updates = map[string]string{}
	}
	p.updates[ref.MessageID] = text
	if p.failUpdates {
		return fmt.Errorf("simulated edit failure")
	}
	return nil
}
func (p *interactionTestPlatform) questionUpdate(id string) string {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	return p.updates[id]
}

type interactionDecision struct {
	id     string
	result PermissionResult
}
type interactionTestAgent struct {
	queueTestAgent
	decisions chan interactionDecision
}

func (a *interactionTestAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	s, err := a.queueTestAgent.StartSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &interactionTestSession{queueTestSession: s.(*queueTestSession), decisions: a.decisions}, nil
}

type interactionTestSession struct {
	*queueTestSession
	decisions chan interactionDecision
}

func (s *interactionTestSession) RespondPermission(id string, result PermissionResult) error {
	s.decisions <- interactionDecision{id, result}
	return nil
}
func interactionTokens(p interface{ getSent() []string }) []string {
	matches := regexp.MustCompile(`/(?:approve|answer) ([a-f0-9]{32})`).FindAllStringSubmatch(strings.Join(p.getSent(), "\n"), -1)
	var result []string
	for _, match := range matches {
		if len(result) == 0 || result[len(result)-1] != match[1] {
			result = append(result, match[1])
		}
	}
	return result
}
func waitInteraction(t *testing.T, p interface{ getSent() []string }, n int) string {
	t.Helper()
	waitQueue(t, func() bool { return len(interactionTokens(p)) >= n })
	return interactionTokens(p)[n-1]
}
func nextInteractionDecision(t *testing.T, a *interactionTestAgent) interactionDecision {
	t.Helper()
	select {
	case d := <-a.decisions:
		return d
	case <-time.After(3 * time.Second):
		t.Fatal("no Agent interaction response")
		return interactionDecision{}
	}
}
func noInteractionDecision(t *testing.T, a *interactionTestAgent) {
	t.Helper()
	select {
	case d := <-a.decisions:
		t.Fatalf("unexpected Agent decision %+v", d)
	case <-time.After(50 * time.Millisecond):
	}
}

func interactionResumeCommand(t *testing.T, p *interactionTestPlatform) string {
	t.Helper()
	matches := regexp.MustCompile(`/resume [^\s]+`).FindAllString(strings.Join(p.getSent(), "\n"), -1)
	if len(matches) == 0 {
		t.Fatal("missing explicit resume entry")
	}
	return matches[len(matches)-1]
}

func sharedQuestionReplies(t *testing.T) {
	a := &interactionTestAgent{queueTestAgent: queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}, decisions: make(chan interactionDecision, 10)}
	p := &interactionTestPlatform{linkTestPlatform: linkTestPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	send := func(user, id, text string, ref *MessageReference, response *InteractionResponse) {
		e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: user, UserID: user, MessageID: id, Content: text, ReplyCtx: user, BotReply: ref, Interaction: response})
	}
	send("alice", "1", "/new Alpha", nil, nil)
	send("alice", "2", "work", nil, nil)
	run := nextQueueSession(t, &a.queueTestAgent)
	<-run.sent
	run.events <- Event{Type: EventPermissionRequest, RequestID: "questions", ToolName: "AskUserQuestion", Questions: []UserQuestion{
		{Question: "Theme?", Options: []UserQuestionOption{{Label: "light"}, {Label: "dark"}}},
		{Question: "Features?", MultiSelect: true, Options: []UserQuestionOption{{Label: "blog"}, {Label: "shop"}, {Label: "gallery"}}},
		{Question: "Notes?"},
	}}
	waitQueue(t, func() bool { return p.receiptCount() == 3 })
	token := waitInteraction(t, p, 1)
	prompts := []string{}
	for _, text := range p.getSent() {
		if strings.Contains(text, "/answer "+token) {
			prompts = append(prompts, text)
		}
	}
	if len(prompts) != 3 {
		t.Fatalf("want one message per question, got %v", prompts)
	}
	for i, question := range []string{"Theme?", "Features?", "Notes?"} {
		if !strings.Contains(prompts[i], question) {
			t.Fatal(prompts)
		}
		for j, other := range []string{"Theme?", "Features?", "Notes?"} {
			if j != i && strings.Contains(prompts[i], other) {
				t.Fatal("questions share a reply target")
			}
		}
	}
	p.authMu.Lock()
	buttons := p.buttonSets
	p.authMu.Unlock()
	if len(buttons) != 3 || len(buttons[0]) != 2 || len(buttons[1]) != 0 || len(buttons[2]) != 0 {
		t.Fatalf("unexpected question buttons: %+v", buttons)
	}
	ref := func(n int) *MessageReference {
		return &MessageReference{Scope: "group", MessageID: fmt.Sprintf("bot-%d", n)}
	}
	send("bob", "3", "wrong owner", ref(3), nil)
	noInteractionDecision(t, a)
	if p.questionUpdate("bot-3") != "" {
		t.Fatal("unauthorized answer edited the question")
	}
	send("alice", "4", "/new Beta", nil, nil)
	send("alice", "5", "/srv/site", ref(3), nil)
	if got := p.questionUpdate("bot-3"); !strings.Contains(got, "/srv/site") {
		t.Fatalf("text answer not reflected: %s", got)
	}
	if p.questionUpdate("bot-1") != "" {
		t.Fatal("unanswered choice was edited")
	}
	send("alice", "6", "overwrite answer", ref(3), nil)
	send("alice", "7", "1,3", ref(2), nil)
	noInteractionDecision(t, a)
	send("alice", "8", "", nil, &InteractionResponse{Token: token, Action: "option", Question: 0, Option: 1})
	if got := p.questionUpdate("bot-1"); !strings.Contains(got, "dark") || strings.Contains(got, "/answer") {
		t.Fatalf("choice not finalized: %s", got)
	}
	if got := p.questionUpdate("bot-3"); strings.Contains(got, "overwrite answer") {
		t.Fatal("duplicate answer edited the question")
	}
	decision := nextInteractionDecision(t, a)
	answers := decision.result.UpdatedInput["answers"].(map[string]any)
	if answers["Theme?"] != "dark" || answers["Features?"] != "blog, gallery" || answers["Notes?"] != "/srv/site" {
		t.Fatal(answers)
	}
	send("alice", "9", "stale answer", ref(2), nil)
	noInteractionDecision(t, a)
	run.events <- Event{Type: EventResult, Content: "done", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "done") })
	noQueueSession(t, &a.queueTestAgent)
}

func sharedQuestionReplyInvalidation(t *testing.T) {
	for _, mode := range []string{"stop", "restart", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			a := &interactionTestAgent{queueTestAgent: queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}, decisions: make(chan interactionDecision, 10)}
			p := &interactionTestPlatform{linkTestPlatform: linkTestPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}}
			path := filepath.Join(t.TempDir(), "sessions")
			e := NewEngine("project", a, []Platform{p}, path, LangEnglish)
			t.Cleanup(func() { _ = e.Stop() })
			send := func(id, text string, ref *MessageReference) {
				e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: id, Content: text, ReplyCtx: "alice", BotReply: ref})
			}
			send("1", "/new Alpha", nil)
			send("2", "work", nil)
			run := nextQueueSession(t, &a.queueTestAgent)
			<-run.sent
			run.events <- Event{Type: EventPermissionRequest, RequestID: "question", ToolName: "AskUserQuestion", Questions: []UserQuestion{{Question: "Text?"}}}
			waitQueue(t, func() bool { return p.receiptCount() == 1 })
			switch mode {
			case "stop":
				send("3", "/stop", nil)
			case "restart":
				if err := e.Stop(); err != nil {
					t.Fatal(err)
				}
				e = NewEngine("project", a, []Platform{p}, path, LangEnglish)
			case "revoked":
				p.deny("alice")
			}
			before := len(p.getSent())
			send("4", "must not submit new work", &MessageReference{Scope: "group", MessageID: "bot-1"})
			got := strings.Join(p.getSent()[before:], "\n")
			want := e.i18n.T(MsgInteractionStale)
			if mode == "revoked" {
				want = e.i18n.T(MsgSharedAccessDenied)
			}
			if !strings.Contains(got, want) {
				t.Fatal(got)
			}
			noInteractionDecision(t, a)
			noQueueSession(t, &a.queueTestAgent)
		})
	}
}

// The message is visible before its send receipt is processed.
type earlyInteractionPlatform struct {
	interactionTestPlatform
	onVisible func(string, [][]ButtonOption)
}

func (p *earlyInteractionPlatform) SendWithButtonsWithReceipt(ctx context.Context, target any, text string, buttons [][]ButtonOption, record func(MessageReference) error) error {
	return p.interactionTestPlatform.SendWithButtonsWithReceipt(ctx, target, text, buttons, func(ref MessageReference) error {
		p.onVisible(text, buttons)
		return record(ref)
	})
}

func TestSharedInteractionResponseBeforeSendReceipt(t *testing.T) {
	for _, kind := range []string{"approval", "single choice", "fallback", "edit failure"} {
		t.Run(kind, func(t *testing.T) {
			a := &interactionTestAgent{queueTestAgent: queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}, decisions: make(chan interactionDecision, 10)}
			p := &earlyInteractionPlatform{interactionTestPlatform: interactionTestPlatform{linkTestPlatform: linkTestPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}}}
			e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangEnglish)
			t.Cleanup(func() { _ = e.Stop() })
			p.failUpdates = kind == "edit failure"
			p.onVisible = func(text string, buttons [][]ButtonOption) {
				token := strings.Split(buttons[0][0].Data, ":")[1]
				msg := &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: "answer", ReplyCtx: "alice"}
				switch kind {
				case "approval":
					msg.Interaction = &InteractionResponse{Token: token, Action: "allow"}
				case "single choice":
					msg.Interaction = &InteractionResponse{Token: token, Action: "option", Question: 0, Option: 0}
				default:
					msg.Content = "/answer " + token + " 1 first"
				}
				e.ReceiveMessage(p, msg)
			}
			for i, text := range []string{"/new Alpha", "work"} {
				e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: fmt.Sprint(i), Content: text, ReplyCtx: "alice"})
			}
			run := nextQueueSession(t, &a.queueTestAgent)
			<-run.sent
			event := Event{Type: EventPermissionRequest, RequestID: "early", ToolName: "Bash"}
			if kind != "approval" {
				event.ToolName = "AskUserQuestion"
				event.Questions = []UserQuestion{{Question: "Choice?", Options: []UserQuestionOption{{Label: "first"}}}}
			}
			run.events <- event
			d := nextInteractionDecision(t, a)
			if kind != "approval" {
				waitQueue(t, func() bool { return strings.Contains(p.questionUpdate("bot-1"), "first") })
			}
			if d.id != "early" || d.result.Behavior != "allow" {
				t.Fatal(d)
			}
			run.events <- Event{Type: EventResult, Content: "completed", Done: true}
			waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "completed") })
		})
	}
}
