package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Hold the final platform response across shutdown, keeping the real turn
// processor alive until the test allows it to finish its session writes.
type shutdownBlockingPlatform struct {
	stubPlatformEngine
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (p *shutdownBlockingPlatform) Send(ctx context.Context, replyCtx any, content string) error {
	p.once.Do(func() {
		close(p.entered)
		<-ctx.Done()
		close(p.cancelled)
		<-p.release
	})
	return p.stubPlatformEngine.Send(ctx, replyCtx, content)
}

func TestEngine_StopWaitsForActiveTurnBeforeStoreCleanup(t *testing.T) {
	dir := t.TempDir()
	p := &shutdownBlockingPlatform{
		stubPlatformEngine: stubPlatformEngine{n: "test"},
		entered:            make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}),
	}
	e := NewEngine("shutdown", &cujAgent{}, []Platform{p}, filepath.Join(dir, "sessions.json"), LangEnglish)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(p.release) }) }
	t.Cleanup(release)
	e.ReceiveMessage(p, &Message{SessionKey: "test:shutdown", Platform: "test", UserID: "user", Content: "hello", ReplyCtx: "ctx"})
	select {
	case <-p.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("agent response never reached platform")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- e.Stop() }()
	select {
	case <-p.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel the active platform call")
	}
	select {
	case err := <-stopped:
		release()
		t.Fatalf("Stop returned while the turn was still running: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not finish after the platform call returned")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove session store after Stop: %v", err)
	}
}

func TestEngine_StopRejectsLateMessagesWithoutRecreatingStore(t *testing.T) {
	dir := t.TempDir()
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("shutdown", &cujAgent{}, []Platform{p}, filepath.Join(dir, "sessions.json"), LangEnglish)
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	e.ReceiveMessage(p, &Message{SessionKey: "test:late", Platform: "test", UserID: "user", Content: "late message", ReplyCtx: "ctx"})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("message received after Stop recreated the store: %v", err)
	}
	if got := p.getSent(); len(got) != 0 {
		t.Fatalf("reply after Stop: %v", got)
	}
}

func TestEngine_StopRejectsScheduledTurns(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("shutdown", &cujAgent{}, []Platform{p}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"cron":      func() error { return e.ExecuteCronJob(&CronJob{SessionKey: "test:late"}) },
		"timer":     func() error { return e.ExecuteTimerJob(&TimerJob{SessionKey: "test:late"}) },
		"heartbeat": func() error { return e.ExecuteHeartbeat("test:late", "late heartbeat", false) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("scheduled turn after Stop = %v, want context.Canceled", err)
			}
		})
	}
	if got := p.getSent(); len(got) != 0 {
		t.Fatalf("scheduled reply after Stop: %v", got)
	}
}
