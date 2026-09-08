package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type mediaLinkPlatform struct {
	linkTestPlatform
	updateMu sync.Mutex
	updates  int
	failFile bool
}

func (p *mediaLinkPlatform) SendImageWithReceipt(ctx context.Context, target any, img ImageAttachment, record func(MessageReference) error) error {
	return p.ReplyWithReceipt(ctx, target, "image:"+img.FileName, record)
}
func (p *mediaLinkPlatform) SendFileWithReceipt(ctx context.Context, target any, file FileAttachment, record func(MessageReference) error) error {
	if p.failFile {
		return fmt.Errorf("file delivery failed")
	}
	return p.ReplyWithReceipt(ctx, target, "file:"+file.FileName, record)
}
func (p *mediaLinkPlatform) SendPreviewWithReceipt(ctx context.Context, target any, text string, record func(MessageReference) error) (any, error) {
	var ref MessageReference
	err := p.ReplyWithReceipt(ctx, target, "preview:"+text, func(r MessageReference) error { ref = r; return record(r) })
	return ref, err
}
func (p *mediaLinkPlatform) UpdatePreviewWithReceipt(ctx context.Context, handle any, text string, record func(MessageReference) error) error {
	p.updateMu.Lock()
	p.updates++
	p.updateMu.Unlock()
	return record(handle.(MessageReference))
}
func (p *mediaLinkPlatform) updateCount() int {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.updates
}

type mediaLinkAgent struct {
	queueTestAgent
	outputKeys chan string
}

func (a *mediaLinkAgent) StartSessionWithEnv(ctx context.Context, id string, env []string) (AgentSession, error) {
	var key string
	for _, v := range env {
		if strings.HasPrefix(v, "CC_SESSION_KEY=") {
			key = strings.TrimPrefix(v, "CC_SESSION_KEY=")
		}
	}
	if key == "" {
		return nil, fmt.Errorf("no immutable media destination")
	}
	a.outputKeys <- key
	return a.StartSession(ctx, id)
}

func TestSharedOutput_PartialMediaAndSilentReplyDoNotReplay(t *testing.T) {
	a := &mediaLinkAgent{queueTestAgent: queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}, outputKeys: make(chan string, 10)}
	p := &mediaLinkPlatform{linkTestPlatform: linkTestPlatform{queueTestPlatform: queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}, failFile: true}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions"), LangEnglish)
	e.attachmentSendEnabled = true
	t.Cleanup(func() { _ = e.Stop() })
	send := func(id, text string, ref *MessageReference) {
		e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: id, Content: text, ReplyCtx: "alice", BotReply: ref})
	}
	send("1", "/new Alpha", nil)
	send("2", "send media", nil)
	s := nextQueueSession(t, &a.queueTestAgent)
	<-s.sent
	key := <-a.outputKeys
	err := e.SendToSessionWithAttachments(key, "", []ImageAttachment{{FileName: "sent.png", Data: []byte("png")}}, []FileAttachment{{FileName: "failed.txt", Data: []byte("txt")}}, nil, false)
	if err == nil {
		t.Fatal("file failure hidden")
	}
	s.events <- Event{Type: EventText, Content: "NO_"}
	s.events <- Event{Type: EventText, Content: "REPLY"}
	s.events <- Event{Type: EventResult, Done: true, Content: "NO_REPLY"}
	// A successfully recorded image remains a continuation target despite the
	// later file failure; only an explicit new user request can start more work.
	noQueueSession(t, &a.queueTestAgent)
	send("2", "duplicate", nil)
	send("3", "reply to failed file", &MessageReference{Scope: "group", MessageID: "bot-2"})
	noQueueSession(t, &a.queueTestAgent)
	send("4", "reply to delivered image", &MessageReference{Scope: "group", MessageID: "bot-1"})
	s = nextQueueSession(t, &a.queueTestAgent)
	<-s.sent
	<-a.outputKeys
	s.events <- Event{Type: EventResult, Done: true, Content: "followup done"}
	waitQueue(t, func() bool { return p.receiptCount() == 2 })
	got := strings.Join(p.getSent(), "\n")
	if strings.Contains(got, "NO_REPLY") || strings.Contains(got, "failed.txt") || strings.Count(got, "image:sent.png") != 1 || !strings.Contains(got, e.i18n.T(MsgSharedReplyUnavailable)) {
		t.Fatal(got)
	}
}
