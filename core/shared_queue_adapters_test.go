package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/agent/claudecode"
	"github.com/chenhg5/cc-connect/agent/codex"
	"github.com/chenhg5/cc-connect/core"
)

type durableAdapterPlatform struct{ replies chan string }

func (p *durableAdapterPlatform) Name() string                    { return "test" }
func (p *durableAdapterPlatform) Start(core.MessageHandler) error { return nil }
func (p *durableAdapterPlatform) Stop() error                     { return nil }
func (p *durableAdapterPlatform) Reply(_ context.Context, target any, s string) error {
	p.replies <- fmt.Sprint(target) + ":" + s
	return nil
}
func (p *durableAdapterPlatform) Send(ctx context.Context, target any, s string) error {
	return p.Reply(ctx, target, s)
}
func (p *durableAdapterPlatform) MarshalReplyContext(target any) (json.RawMessage, error) {
	return json.Marshal(target)
}
func (p *durableAdapterPlatform) UnmarshalReplyContext(b json.RawMessage) (any, error) {
	var s string
	err := json.Unmarshal(b, &s)
	return s, err
}

// Real adapters and local processes; only the CLI's external protocol is simulated.
func TestSharedQueue_ClaudeAndCodexAdapterWiring(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX protocol fixtures")
	}
	cases := []struct {
		name, script string
		factory      func(map[string]any) (core.Agent, error)
	}{
		{"claude", `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"shared-history"}'
while IFS= read -r line; do
 printf '%s\n' '{"type":"result","subtype":"success","session_id":"shared-history","result":"adapter answer","is_error":false}'
done
`, claudecode.New},
		{"codex", `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"shared-history"}' '{"type":"item.completed","item":{"type":"agent_message","text":"adapter answer"}}' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
`, codex.New},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, tc.name)
			if err := os.WriteFile(bin, []byte(tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			ag, err := tc.factory(map[string]any{"cmd": bin, "work_dir": dir, "mode": "default", "backend": "exec"})
			if err != nil {
				t.Fatal(err)
			}
			p := &durableAdapterPlatform{replies: make(chan string, 32)}
			e := core.NewEngine("project", ag, []core.Platform{p}, filepath.Join(t.TempDir(), "state"), core.LangEnglish)
			t.Cleanup(func() { _ = e.Stop() })
			send := func(id, content string) {
				e.ReceiveMessage(p, &core.Message{Platform: "test", SharedScope: "group", SessionKey: "alice", UserID: "alice", MessageID: id, Content: content, ReplyCtx: "topic-one"})
			}
			send("1", "/new Alpha")
			send("2", "first task")
			send("3", "second task")
			answers := 0
			deadline := time.After(10 * time.Second)
			for answers < 2 {
				select {
				case got := <-p.replies:
					if strings.Contains(got, "Queue paused") {
						t.Fatal(got)
					}
					if got == "topic-one:adapter answer" {
						answers++
					}
				case <-deadline:
					t.Fatal("missing adapter answers")
				}
			}
		})
	}
}
