//go:build unix

package core_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/agent/claudecode"
	"github.com/chenhg5/cc-connect/agent/codex"
	"github.com/chenhg5/cc-connect/core"
)

// A real host process is killed while a real adapter's fake external CLI remains
// alive in its own process group. This is process-level automated evidence, not
// a real Claude/Codex or Telegram collaboration acceptance test.
func TestSharedRecovery_RealProcessGroupSurvivesHostCrash(t *testing.T) {
	if dir := os.Getenv("CC_RECOVERY_PROCESS_FIXTURE"); dir != "" {
		fixtureRecoveryHost(t, dir, os.Getenv("CC_RECOVERY_AGENT"))
		return
	}
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) { testRecoveryProcessGroup(t, kind) })
	}
}

func recoveryProcessEngine(t *testing.T, dir, kind string) (*core.Engine, *durableAdapterPlatform) {
	t.Helper()
	factory := codex.New
	if kind == "claude" {
		factory = claudecode.New
	}
	agent, err := factory(map[string]any{"cmd": filepath.Join(dir, "fake-cli"), "work_dir": dir, "mode": "default", "backend": "exec"})
	if err != nil {
		t.Fatal(err)
	}
	p := &durableAdapterPlatform{replies: make(chan string, 64)}
	e := core.NewEngine("project", agent, []core.Platform{p}, filepath.Join(dir, "state"), core.LangEnglish)
	return e, p
}

func recoverySend(e *core.Engine, p *durableAdapterPlatform, user, id, content string) {
	e.ReceiveMessage(p, &core.Message{Platform: "test", SharedScope: "group", SessionKey: user, UserID: user, MessageID: id, Content: content, ReplyCtx: "current-topic"})
}

func waitRecoveryReply(t *testing.T, p *durableAdapterPlatform, want string) string {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-p.replies:
			if strings.Contains(got, want) {
				return got
			}
		case <-timeout:
			t.Fatalf("missing reply %q", want)
			return ""
		}
	}
}

func waitRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing external process observation %s", path)
	return nil
}

func fixtureRecoveryHost(t *testing.T, dir, kind string) {
	e, p := recoveryProcessEngine(t, dir, kind)
	recoverySend(e, p, "alice", "1", "/new Alpha")
	recoverySend(e, p, "alice", "2", "first task")
	waitRecoveryFile(t, filepath.Join(dir, "began"))
	recoverySend(e, p, "alice", "3", "queued second")
	waitRecoveryReply(t, p, "1 request(s) ahead")
	fmt.Println("RECOVERY_HOST_READY")
	select {}
}

func testRecoveryProcessGroup(t *testing.T, kind string) {
	dir := t.TempDir()
	protocol := "printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"shared-history\"}'\ncat >/dev/null\n"
	result := "printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"second completed\"}}' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if kind == "claude" {
		protocol = "printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"shared-history\"}'\nIFS= read -r line\n"
		result = "printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"shared-history\",\"result\":\"second completed\",\"is_error\":false}'\ncat >/dev/null\n"
	}
	script := "#!/bin/sh\nprintf '%s\\n' $$ > executor.pid\n" + protocol + "printf 'execution\\n' >> executions\nif [ ! -e began ]; then printf 'started\\n' > began; exec sleep 60; fi\n" + result
	if err := os.WriteFile(filepath.Join(dir, "fake-cli"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedRecovery_RealProcessGroupSurvivesHostCrash$")
	cmd.Env = append(os.Environ(), "CC_RECOVERY_PROCESS_FIXTURE="+dir, "CC_RECOVERY_AGENT="+kind)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	ready := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if scan.Text() == "RECOVERY_HOST_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("host failed before checkpoint")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("host did not reach checkpoint")
	}
	group, err := strconv.Atoi(strings.TrimSpace(string(waitRecoveryFile(t, filepath.Join(dir, "executor.pid")))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
	if pgid, err := syscall.Getpgid(group); err != nil || pgid != group {
		t.Fatalf("executor not isolated: group=%d pgid=%d err=%v", group, pgid, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	e, p := recoveryProcessEngine(t, dir, kind)
	t.Cleanup(func() { _ = e.Stop() })
	if err = e.Start(); err != nil {
		t.Fatal(err)
	}
	recoverySend(e, p, "bob", "4", "/switch 1")
	recoverySend(e, p, "bob", "5", "/queue")
	listing := waitRecoveryReply(t, p, "/resolve ")
	request := ""
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, "/resolve ") {
			request = strings.TrimPrefix(line, "/resolve ")
		}
	}
	if request == "" {
		t.Fatal(listing)
	}
	recoverySend(e, p, "bob", "6", "/resolve "+request)
	waitRecoveryReply(t, p, "executor exit or interruption handling is still pending")
	if count := strings.Count(string(waitRecoveryFile(t, filepath.Join(dir, "executions"))), "execution"); count != 1 {
		t.Fatalf("replayed before resolution: %d", count)
	}
	if err = syscall.Kill(-group, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) {
		t.Fatal("executor group exit not confirmed")
	}
	recoverySend(e, p, "bob", "7", "/resolve "+request)
	waitRecoveryReply(t, p, "preserved existing work")
	recoverySend(e, p, "bob", "8", "/resume "+request)
	waitRecoveryReply(t, p, "second completed")
	if count := strings.Count(string(waitRecoveryFile(t, filepath.Join(dir, "executions"))), "execution"); count != 2 {
		t.Fatalf("expected only queued task after recovery, got %d executions", count)
	}
	recoverySend(e, p, "bob", "9", "/resolve "+request)
	waitRecoveryReply(t, p, "Control expired")
}
