package core_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/agent/claudecode"
	"github.com/chenhg5/cc-connect/agent/codex"
	"github.com/chenhg5/cc-connect/core"
)

// Concurrent real local subprocesses must receive their own routing identity,
// and neither start may overwrite the agent's legacy environment defaults.
func TestSharedOutput_ConcurrentAgentProcessEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixtures")
	}
	for _, tc := range []struct {
		name, script string
		factory      func(map[string]any) (core.Agent, error)
	}{
		{"claude", `#!/bin/sh
printf '%s|%s' "$CC_PROJECT" "$CC_SESSION_KEY" > "$CC_ROUTE_CAPTURE"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"history"}'
while IFS= read -r line; do
 printf '%s\n' '{"type":"result","subtype":"success","session_id":"history","result":"done","is_error":false}'
done
`, claudecode.New},
		{"codex", `#!/bin/sh
printf '%s|%s' "$CC_PROJECT" "$CC_SESSION_KEY" > "$CC_ROUTE_CAPTURE"
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"history"}' '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
`, codex.New},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "agent")
			if err := os.WriteFile(bin, []byte(tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			a, err := tc.factory(map[string]any{"cmd": bin, "work_dir": dir, "mode": "default", "backend": "exec"})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Stop()
			a.(core.SessionEnvInjector).SetSessionEnv([]string{"CC_PROJECT=legacy", "CC_SESSION_KEY=legacy", "CC_ROUTE_CAPTURE=" + filepath.Join(dir, "legacy")})
			run := func(n int, scoped bool) error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var s core.AgentSession
				var err error
				if scoped {
					s, err = a.(core.SessionEnvStarter).StartSessionWithEnv(ctx, "", []string{fmt.Sprintf("CC_PROJECT=project-%d", n), fmt.Sprintf("CC_SESSION_KEY=shared-request:%d", n), "CC_ROUTE_CAPTURE=" + filepath.Join(dir, fmt.Sprint(n))})
				} else {
					s, err = a.StartSession(ctx, "")
				}
				if err != nil {
					return err
				}
				defer s.Close()
				if err := s.Send("work", "message", nil, nil); err != nil {
					return err
				}
				for {
					select {
					case event, ok := <-s.Events():
						if !ok {
							return fmt.Errorf("no result")
						}
						if event.Type == core.EventResult && event.Done {
							return nil
						}
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for i := 1; i <= 2; i++ {
				wg.Add(1)
				go func(i int) { defer wg.Done(); errs <- run(i, true) }(i)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := run(0, false); err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{"1", "2", "legacy"} {
				data, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				want := "legacy|legacy"
				if file != "legacy" {
					want = "project-" + file + "|shared-request:" + file
				}
				if strings.TrimSpace(string(data)) != want {
					t.Fatalf("%s got %q want %q", file, data, want)
				}
			}
		})
	}
}
