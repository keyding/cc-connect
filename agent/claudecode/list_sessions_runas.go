package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func listSessionsAsUser(ctx context.Context, user, workDir string) ([]core.AgentSessionInfo, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("claudecode: locate session reader: %w", err)
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("claudecode: resolve work_dir: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Reuse the installed executable, so the isolated reader needs no shell
	// parser script, extra runtime, or access to the supervisor's credentials.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-iu", user, "--", executable, "_claude-list-sessions", absWorkDir)
	cmd.Env = core.FilterEnvForSpawn(os.Environ(), core.SpawnOptions{RunAsUser: user})
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claudecode: list sessions as %q: %w", user, err)
	}
	var sessions []core.AgentSessionInfo
	if err := json.Unmarshal(output, &sessions); err != nil {
		return nil, fmt.Errorf("claudecode: invalid isolated session list: %w", err)
	}
	return sessions, nil
}

func deleteSessionAsUser(ctx context.Context, user, workDir, id string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("claudecode: prepare isolated deletion: %w", err)
	}
	dir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("claudecode: prepare isolated deletion: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-iu", user, "--", executable, "_claude-delete-session", dir, id)
	cmd.Env = core.FilterEnvForSpawn(os.Environ(), core.SpawnOptions{RunAsUser: user})
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("claudecode: delete isolated session: %w", err)
	}
	return nil
}
