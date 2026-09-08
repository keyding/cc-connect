package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type linkTestPlatform struct {
	queueTestPlatform
	receiptMu   sync.Mutex
	receipts    []MessageReference
	failReceipt bool
}

func (p *linkTestPlatform) ReplyWithReceipt(ctx context.Context, target any, text string, record func(MessageReference) error) error {
	if err := p.Reply(ctx, target, text); err != nil {
		return err
	}
	p.receiptMu.Lock()
	defer p.receiptMu.Unlock()
	ref := MessageReference{Scope: "group", MessageID: fmt.Sprintf("bot-%d", len(p.receipts)+1)}
	if p.failReceipt {
		return fmt.Errorf("receipt lost")
	}
	if err := record(ref); err != nil {
		return err
	}
	p.receipts = append(p.receipts, ref)
	return nil
}
func (p *linkTestPlatform) receiptCount() int {
	p.receiptMu.Lock()
	defer p.receiptMu.Unlock()
	return len(p.receipts)
}

func TestSharedReply_LostReceiptNeverReexecutesOrGuessesDefault(t *testing.T) {
	a := &queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}
	p := &linkTestPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}, failReceipt: true}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	send := func(id, text string, ref *MessageReference) {
		e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: id, Content: text, ReplyCtx: "alice", BotReply: ref})
	}
	send("1", "/new Alpha", nil)
	send("2", "work", nil)
	s := nextQueueSession(t, a)
	<-s.sent
	s.events <- Event{Type: EventResult, Done: true, Content: "delivered but receipt lost"}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "delivered but receipt lost") })
	send("2", "duplicate", nil)
	send("3", "continue", &MessageReference{Scope: "group", MessageID: "bot-1"})
	noQueueSession(t, a)
	got := strings.Join(p.getSent(), "\n")
	if !strings.Contains(got, e.i18n.T(MsgSharedReplyUnavailable)) || strings.Count(got, "delivered but receipt lost") != 1 {
		t.Fatal(got)
	}
}
