//go:build !full_plugins && !no_claudecode && !no_codex && !no_telegram

package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestDefaultBuild_OnlyTelegramClaudeAndCodex(t *testing.T) {
	agents := core.ListRegisteredAgents()
	sort.Strings(agents)
	if !reflect.DeepEqual(agents, []string{"claudecode", "codex"}) {
		t.Fatalf("registered agents = %v", agents)
	}
	platforms := core.ListRegisteredPlatforms()
	if !reflect.DeepEqual(platforms, []string{"telegram"}) {
		t.Fatalf("registered platforms = %v", platforms)
	}
}
