//go:build unix

package claudecode

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrepareCmdForKill_SetsSetpgid(t *testing.T) {
	cmd := exec.Command("/bin/true")
	prepareCmdForKill(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil after prepareCmdForKill")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid not set after prepareCmdForKill")
	}
}

func TestPrepareCmdForKill_PreservesExistingSysProcAttr(t *testing.T) {
	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Foreground: false}
	prepareCmdForKill(cmd)
	if !cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid not set when SysProcAttr was pre-populated")
	}
}

func TestPrepareCmdForKill_NilCmd(t *testing.T) {
	// Must not panic on a nil *exec.Cmd.
	prepareCmdForKill(nil)
}

func TestForceKillCmd_NoProcess(t *testing.T) {
	cmd := exec.Command("/bin/true")
	// cmd has not been Start()ed, so cmd.Process is nil.
	if err := forceKillCmd(cmd); err != nil {
		t.Errorf("expected no error on un-started cmd, got %v", err)
	}
}

func TestForceKillCmd_NilCmd(t *testing.T) {
	if err := forceKillCmd(nil); err != nil {
		t.Errorf("expected no error on nil cmd, got %v", err)
	}
}

// The shell and its grandchild both inherit the pipe's write end. EOF on
// the read end proves that the grandchild was terminated too. Unlike
// Cmd.StdoutPipe, this pipe is not closed by Cmd.Wait on the test's behalf.
func TestForceKillCmd_KillsGrandchild(t *testing.T) {
	stdout, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdout.Close() })
	t.Cleanup(func() { _ = writer.Close() })

	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & echo $! ; wait")
	prepareCmdForKill(cmd)
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waited := false
	t.Cleanup(func() {
		// Also clean up the grandchild if an assertion fails after the shell exits.
		if t.Failed() || !waited {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if !waited {
			_ = cmd.Wait()
		}
	})
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stdout.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read grandchild PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid grandchild PID %q", line)
	}

	if err := forceKillCmd(cmd); err != nil {
		t.Fatalf("forceKillCmd: %v", err)
	}
	_ = cmd.Wait()
	waited = true

	// Do not signal the group again here: on macOS an exiting group can
	// contain only zombies and temporarily return EPERM instead of ESRCH.
	if err := stdout.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("grandchild %d still holds stdout after group kill: %v", pid, err)
	}
}

func TestForceKillCmd_AlreadyReapedProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	prepareCmdForKill(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := forceKillCmd(cmd); err != nil {
		t.Fatalf("kill of already reaped process group should be a no-op: %v", err)
	}
}

func TestSignalProcessGroup_NoProcess(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if err := signalProcessGroup(cmd, syscall.SIGTERM); err != nil {
		t.Errorf("expected no error on un-started cmd, got %v", err)
	}
}

func TestSignalProcessGroup_NilCmd(t *testing.T) {
	if err := signalProcessGroup(nil, syscall.SIGTERM); err != nil {
		t.Errorf("expected no error on nil cmd, got %v", err)
	}
}
