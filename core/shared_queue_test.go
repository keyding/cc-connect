package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type queueTestPlatform struct {
	stubPlatformEngine
	muTargets sync.Mutex
	targets   []string
}

func (p *queueTestPlatform) MarshalReplyContext(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}
func (p *queueTestPlatform) UnmarshalReplyContext(v json.RawMessage) (any, error) {
	var s string
	err := json.Unmarshal(v, &s)
	return s, err
}
func (p *queueTestPlatform) Reply(ctx context.Context, target any, content string) error {
	p.muTargets.Lock()
	p.targets = append(p.targets, fmt.Sprint(target)+":"+content)
	p.muTargets.Unlock()
	return p.stubPlatformEngine.Reply(ctx, target, content)
}

type queueTestAgent struct {
	cujAgent
	dir         string
	calls       chan *queueTestSession
	sendRelease <-chan struct{}
	sendError   error
	exitRelease <-chan struct{}
}

func (a *queueTestAgent) GetWorkDir() string { return a.dir }
func (a *queueTestAgent) StartSession(ctx context.Context, id string) (AgentSession, error) {
	s := &queueTestSession{cujAgentSession: newCUJAgentSession(), resume: id, sent: make(chan string, 1), sendRelease: a.sendRelease, sendError: a.sendError, exitRelease: a.exitRelease}
	a.calls <- s
	return s, nil
}

type queueTestSession struct {
	*cujAgentSession
	resume      string
	sent        chan string
	files       []FileAttachment
	sendRelease <-chan struct{}
	sendError   error
	exitRelease <-chan struct{}
}

func (s *queueTestSession) Send(prompt, id string, images []ImageAttachment, files []FileAttachment) error {
	s.files = files
	s.sent <- prompt
	if s.sendRelease != nil {
		<-s.sendRelease
	}
	return s.sendError
}
func (s *queueTestSession) CurrentSessionID() string { return "history-one" }
func nextQueueSession(t *testing.T, a *queueTestAgent) *queueTestSession {
	t.Helper()
	select {
	case s := <-a.calls:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not start")
		return nil
	}
}
func noQueueSession(t *testing.T, a *queueTestAgent) {
	t.Helper()
	select {
	case <-a.calls:
		t.Fatal("unexpected agent execution")
	case <-time.After(100 * time.Millisecond):
	}
}
func queueMessage(e *Engine, p *queueTestPlatform, user, id, content string) {
	e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: user, UserID: user, MessageID: id, Content: content, ReplyCtx: user})
}

func TestSharedQueue_DurableFIFOAndFixedTarget(t *testing.T) {
	a := &queueTestAgent{dir: t.TempDir(), calls: make(chan *queueTestSession, 10)}
	p := &queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := NewEngine("project", a, []Platform{p}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	queueMessage(e, p, "alice", "1", "/new Alpha")
	queueMessage(e, p, "bob", "2", "/switch 1")
	queueMessage(e, p, "alice", "3", "first")
	s := nextQueueSession(t, a)
	if got := <-s.sent; !strings.Contains(got, "first") {
		t.Fatal(got)
	}
	queueMessage(e, p, "bob", "4", "second")
	queueMessage(e, p, "bob", "4", "edited second")
	queueMessage(e, p, "bob", "5", "/new Beta")
	noQueueSession(t, a)
	s.events <- Event{Type: EventResult, Content: "answer one", Done: true}
	next := nextQueueSession(t, a)
	if next.resume != "history-one" {
		t.Fatalf("lost target history: %q", next.resume)
	}
	if got := <-next.sent; !strings.Contains(got, "second") || strings.Contains(got, "edited") {
		t.Fatal(got)
	}
	next.events <- Event{Type: EventResult, Content: "answer two", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "answer two") })
	p.muTargets.Lock()
	targets := strings.Join(p.targets, "\n")
	p.muTargets.Unlock()
	if !strings.Contains(targets, "bob:answer two") {
		t.Fatal(targets)
	}
	noQueueSession(t, a)
}

func waitQueue(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *queueTestSession) WaitForExit(ctx context.Context) error {
	if s.exitRelease != nil {
		select {
		case <-s.exitRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func newQueueEngine(t *testing.T, dir, path string) (*Engine, *queueTestPlatform, *queueTestAgent) {
	t.Helper()
	a := &queueTestAgent{dir: dir, calls: make(chan *queueTestSession, 32)}
	p := &queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := NewEngine("project", a, []Platform{p}, path, LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	return e, p, a
}

func TestSharedQueue_SameActualDirectoryAndIndependentDirectory(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	e, p, a := newQueueEngine(t, dir, filepath.Join(t.TempDir(), "s"))
	other, op, oa := newQueueEngine(t, alias, filepath.Join(t.TempDir(), "s"))
	independent, ip, ia := newQueueEngine(t, t.TempDir(), filepath.Join(t.TempDir(), "s"))
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "first")
	first := nextQueueSession(t, a)
	<-first.sent
	queueMessage(other, op, "b", "1", "/new Beta")
	queueMessage(other, op, "b", "2", "conflict")
	queueMessage(independent, ip, "c", "1", "/new Gamma")
	queueMessage(independent, ip, "c", "2", "independent")
	free := nextQueueSession(t, ia)
	<-free.sent
	first.events <- Event{Type: EventPermissionRequest, RequestID: "question"}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "waiting for interaction") })
	noQueueSession(t, oa)
	free.events <- Event{Type: EventResult, Content: "free done", Done: true}
	first.events <- Event{Type: EventResult, Content: "first done", Done: true}
	second := nextQueueSession(t, oa)
	<-second.sent
	second.events <- Event{Type: EventResult, Content: "second done", Done: true}
}

func TestSharedQueue_RestartKeepsQueuedAttachmentsAndReply(t *testing.T) {
	dir := t.TempDir()
	holder, hp, ha := newQueueEngine(t, dir, filepath.Join(t.TempDir(), "s"))
	queueMessage(holder, hp, "h", "1", "/new Holder")
	queueMessage(holder, hp, "h", "2", "hold")
	held := nextQueueSession(t, ha)
	<-held.sent
	path := filepath.Join(t.TempDir(), "s")
	e, p, a := newQueueEngine(t, dir, path)
	queueMessage(e, p, "alice", "1", "/new Documents")
	files := []FileAttachment{{FileName: "plan.txt", Data: []byte("original attachment")}}
	e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: "2", Content: "read plan", Files: files, ReplyCtx: "original-topic"})
	// The caller owns inbound buffers and may reuse them after ReceiveMessage.
	files[0].Data[0] = 'X'
	noQueueSession(t, a)
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	held.events <- Event{Type: EventResult, Content: "released", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(hp.getSent(), "\n"), "released") })
	restored, rp, ra := newQueueEngine(t, dir, path)
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	s := nextQueueSession(t, ra)
	<-s.sent
	if len(s.files) != 1 || string(s.files[0].Data) != "original attachment" {
		t.Fatalf("lost attachment: %+v", s.files)
	}
	s.events <- Event{Type: EventResult, Content: "read complete", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(rp.getSent(), "\n"), "read complete") })
	rp.muTargets.Lock()
	targets := strings.Join(rp.targets, "\n")
	rp.muTargets.Unlock()
	if !strings.Contains(targets, "original-topic:read complete") {
		t.Fatal(targets)
	}
}

func TestSharedQueue_FailureAndInterruptionSurviveRestart(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			dir, path := t.TempDir(), filepath.Join(t.TempDir(), "s")
			e, p, a := newQueueEngine(t, dir, path)
			queueMessage(e, p, "a", "1", "/new Alpha")
			queueMessage(e, p, "a", "2", "first")
			s := nextQueueSession(t, a)
			<-s.sent
			queueMessage(e, p, "a", "3", "second")
			if failure {
				s.events <- Event{Type: EventError, Error: fmt.Errorf("failed")}
				waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
			}
			if err := e.Stop(); err != nil {
				t.Fatal(err)
			}
			restored, rp, ra := newQueueEngine(t, dir, path)
			if err := restored.Start(); err != nil {
				t.Fatal(err)
			}
			queueMessage(restored, rp, "a", "4", "third")
			if got := strings.Join(rp.getSent(), "\n"); !strings.Contains(got, "Saved for Alpha") || !strings.Contains(got, "Queue paused") {
				t.Fatal(got)
			}
			noQueueSession(t, ra)
		})
	}
}

// Replace the storage parent at the filesystem boundary; no queue layout is inspected.
func breakQueueStorage(t *testing.T, dir string) func() {
	t.Helper()
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(dir+"-saved", dir); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSharedQueue_AdmissionStorageFailureNeverExecutes(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "state")
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(storage, "s"))
	queueMessage(e, p, "a", "1", "/new Alpha")
	restore := breakQueueStorage(t, storage)
	e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "a", UserID: "a", MessageID: "2", Files: []FileAttachment{{FileName: "task.txt", Data: []byte("task")}}, ReplyCtx: "a"})
	restore()
	if got := strings.Join(p.getSent(), "\n"); !strings.Contains(got, "Task not accepted") {
		t.Fatal(got)
	}
	noQueueSession(t, a)
}
func TestSharedQueue_CompletionStorageFailureNeverReplays(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "state")
	dir := t.TempDir()
	path := filepath.Join(storage, "s")
	e, p, a := newQueueEngine(t, dir, path)
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "first")
	s := nextQueueSession(t, a)
	<-s.sent
	queueMessage(e, p, "a", "3", "second")
	restore := breakQueueStorage(t, storage)
	s.events <- Event{Type: EventResult, Content: "completed externally", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
	restore()
	noQueueSession(t, a)
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	restored, rp, ra := newQueueEngine(t, dir, path)
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	queueMessage(restored, rp, "a", "4", "third")
	noQueueSession(t, ra)
	if got := strings.Join(rp.getSent(), "\n"); !strings.Contains(got, "Queue paused") {
		t.Fatal(got)
	}
}

func TestSharedQueue_AdmissionFreezesAttachmentBuffers(t *testing.T) {
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(t.TempDir(), "s"))
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "hold")
	first := nextQueueSession(t, a)
	<-first.sent
	files := []FileAttachment{{FileName: "plan.txt", Data: []byte("original")}}
	e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "a", UserID: "a", MessageID: "3", Content: "read", Files: files, ReplyCtx: "a"})
	files[0].Data[0] = 'X'
	first.events <- Event{Type: EventResult, Content: "done", Done: true}
	second := nextQueueSession(t, a)
	<-second.sent
	if string(second.files[0].Data) != "original" {
		t.Fatal("queued attachment changed after admission")
	}
	second.events <- Event{Type: EventResult, Content: "done", Done: true}
}

func TestSharedQueue_ResultDoesNotHideSendFailure(t *testing.T) {
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(t.TempDir(), "s"))
	release := make(chan struct{})
	a.sendRelease = release
	a.sendError = fmt.Errorf("send failed")
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "first")
	s := nextQueueSession(t, a)
	<-s.sent
	queueMessage(e, p, "a", "3", "second")
	s.events <- Event{Type: EventResult, Content: "apparent result", Done: true}
	// Release before cleanup even if an assertion fails.
	time.Sleep(50 * time.Millisecond)
	close(release)
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
	noQueueSession(t, a)
}

func TestSharedQueue_ExecutorExitRequiredBeforeNextTask(t *testing.T) {
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(t.TempDir(), "s"))
	release := make(chan struct{})
	a.exitRelease = release
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "first")
	s := nextQueueSession(t, a)
	<-s.sent
	queueMessage(e, p, "a", "3", "second")
	s.events <- Event{Type: EventResult, Content: "done", Done: true}
	noQueueSession(t, a)
	close(release)
	next := nextQueueSession(t, a)
	<-next.sent
	next.events <- Event{Type: EventResult, Content: "next done", Done: true}
}

func TestSharedQueue_ProcessCrashDoesNotReplay(t *testing.T) {
	if path := os.Getenv("CC_QUEUE_CRASH_FIXTURE"); path != "" {
		e, p, a := newQueueEngine(t, filepath.Dir(path), path)
		queueMessage(e, p, "a", "1", "/new Alpha")
		if mode := os.Getenv("CC_QUEUE_CRASH_START_FAILURE"); mode != "running" {
			var restore func()
			e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "a", UserID: "a", MessageID: "2", Content: "not started", ReplyCtx: "a", OnAccepted: func() {
				if mode == "pending-intent" {
					restore = breakQueueSnapshot(t, path)
				} else {
					restore = breakQueueStorage(t, filepath.Dir(path))
				}
			}})
			waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
			restore()
			noQueueSession(t, a)
		} else {
			queueMessage(e, p, "a", "2", "started")
			s := nextQueueSession(t, a)
			<-s.sent
			queueMessage(e, p, "a", "3", "queued")
		}
		fmt.Println("QUEUE_CRASH_READY")
		select {}
	}
	for _, mode := range []string{"running", "no-intent", "pending-intent"} {
		t.Run(mode, func(t *testing.T) { crashSharedQueue(t, mode) })
	}
}

func crashSharedQueue(t *testing.T, mode string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedQueue_ProcessCrashDoesNotReplay$")
	cmd.Env = append(os.Environ(), "CC_QUEUE_CRASH_FIXTURE="+path, "CC_QUEUE_CRASH_START_FAILURE="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "QUEUE_CRASH_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not reach crash point")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child startup timeout")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	restored, p, a := newQueueEngine(t, dir, path)
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	queueMessage(restored, p, "a", "4", "after actual process crash")
	if mode == "no-intent" {
		first := nextQueueSession(t, a)
		if got := <-first.sent; !strings.Contains(got, "not started") {
			t.Fatal(got)
		}
		first.events <- Event{Type: EventResult, Content: "first completed once", Done: true}
		next := nextQueueSession(t, a)
		<-next.sent
		next.events <- Event{Type: EventResult, Content: "next completed once", Done: true}
		waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "next completed once") })
		noQueueSession(t, a)
		return
	}
	if got := strings.Join(p.getSent(), "\n"); !strings.Contains(got, "Queue paused") || !strings.Contains(got, "Saved for Alpha") {
		t.Fatal(got)
	}
	noQueueSession(t, a)
}

func TestSharedQueue_StartCheckpointFailureDoesNotStartAgent(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "state")
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(storage, "s"))
	queueMessage(e, p, "a", "1", "/new Alpha")
	var restore func()
	e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "a", UserID: "a", MessageID: "2", Content: "first", ReplyCtx: "a", OnAccepted: func() { restore = breakQueueStorage(t, storage) }})
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
	noQueueSession(t, a)
}

func TestSharedQueue_IdleTimeoutPausesFollowingRequests(t *testing.T) {
	e, p, a := newQueueEngine(t, t.TempDir(), filepath.Join(t.TempDir(), "s"))
	e.SetEventIdleTimeout(80 * time.Millisecond)
	queueMessage(e, p, "a", "1", "/new Alpha")
	queueMessage(e, p, "a", "2", "first")
	s := nextQueueSession(t, a)
	<-s.sent
	queueMessage(e, p, "a", "3", "second")
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "Queue paused") })
	noQueueSession(t, a)
}

type failedDeliveryPlatform struct{ *queueTestPlatform }

func (p *failedDeliveryPlatform) Reply(ctx context.Context, target any, content string) error {
	if content == "undeliverable answer" {
		return fmt.Errorf("destination unavailable")
	}
	return p.queueTestPlatform.Reply(ctx, target, content)
}
func TestSharedQueue_DeliveryFailureDoesNotExecuteAgain(t *testing.T) {
	dir, path := t.TempDir(), filepath.Join(t.TempDir(), "s")
	a := &queueTestAgent{dir: dir, calls: make(chan *queueTestSession, 10)}
	p := &failedDeliveryPlatform{&queueTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}
	e := NewEngine("project", a, []Platform{p}, path, LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	send := func(id, content string) {
		e.ReceiveMessage(p, &Message{Platform: "test", SharedScope: "group", SessionKey: "a", UserID: "a", MessageID: id, Content: content, ReplyCtx: "a"})
	}
	send("1", "/new Alpha")
	send("2", "first")
	s := nextQueueSession(t, a)
	<-s.sent
	send("3", "second")
	s.events <- Event{Type: EventResult, Content: "undeliverable answer", Done: true}
	next := nextQueueSession(t, a)
	<-next.sent
	next.events <- Event{Type: EventResult, Content: "delivered", Done: true}
	waitQueue(t, func() bool { return strings.Contains(strings.Join(p.getSent(), "\n"), "delivered") })
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	restored, rp, ra := newQueueEngine(t, dir, path)
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	queueMessage(restored, rp, "a", "2", "first edited")
	noQueueSession(t, ra)
}

// The external agent fixture makes loss of resumed context visible in its reply.
func (s *queueTestSession) answerInResumedConversation(answer string) {
	if s.resume == "" {
		answer = "fresh conversation without prior context"
	}
	s.events <- Event{Type: EventResult, Content: answer, Done: true}
}

// Fail replacement of the primary snapshot after the journal can be written.
// Assertions remain at ReceiveMessage, output and executor boundaries.
func breakQueueSnapshot(t *testing.T, path string) func() {
	t.Helper()
	snapshot := path + ".requests.json"
	if err := os.Rename(snapshot, snapshot+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(snapshot); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(snapshot+"-saved", snapshot); err != nil {
			t.Fatal(err)
		}
	}
}
