//go:build no_claudecode

package main

func runClaudeInternalCommand(string, []string) (int, bool) { return 0, false }
