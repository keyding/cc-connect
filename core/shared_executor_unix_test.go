//go:build unix

package core

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type groupProofAgent struct {
	queueTestAgent
	group int
}

func (a *groupProofAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	if err := CheckpointSharedExecutor(ctx, a.group); err != nil {
		return nil, err
	}
	return a.queueTestAgent.StartSession(ctx, id)
}

func TestSharedRecovery_DirectWaitDoesNotReleaseLiveProcessGroup(t *testing.T) {
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	a := &groupProofAgent{queueTestAgent: queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}, group: child.Process.Pid}
	p := &queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "state"), LangEnglish)
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait(); _ = e.Stop() })
	queueMessage(e, p, "alice", "1", "/new Alpha")
	queueMessage(e, p, "alice", "2", "first")
	first := nextQueueSession(t, &a.queueTestAgent)
	<-first.sent
	queueMessage(e, p, "alice", "3", "second")
	// The stub says its direct process is reaped, but the registered group is live.
	first.events <- Event{Type: EventResult, Content: "first done", Done: true}
	noQueueSession(t, &a.queueTestAgent)
	queueMessage(e, p, "alice", "4", "/queue")
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	next := nextQueueSession(t, &a.queueTestAgent)
	if got := <-next.sent; !strings.Contains(got, "second") {
		t.Fatal(got)
	}
	next.events <- Event{Type: EventResult, Content: "second done", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "second done") })
}
