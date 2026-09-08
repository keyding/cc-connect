//go:build !no_claudecode

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chenhg5/cc-connect/agent/claudecode"
)

func runClaudeInternalCommand(name string, args []string) (int, bool) {
	switch name {
	case "_claude-list-sessions":
		return runClaudeListSessions(args), true
	case "_claude-delete-session":
		return runClaudeDeleteSession(args), true
	default:
		return 0, false
	}
}

func runClaudeListSessions(args []string) int {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		fmt.Fprintln(os.Stderr, "expected one absolute workspace path")
		return 2
	}
	sessions, err := claudecode.ListLocalSessions(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(sessions); err != nil {
		return 1
	}
	return 0
}

func runClaudeDeleteSession(args []string) int {
	if len(args) != 2 || !filepath.IsAbs(args[0]) {
		fmt.Fprintln(os.Stderr, "expected absolute workspace and session ID")
		return 2
	}
	if err := claudecode.DeleteLocalSession(args[0], args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
