package main

import (
	"go/build"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMakeBuild_OnlyRequestedPluginsWithoutWeb(t *testing.T) {
	for _, target := range []string{"build", "build-noweb"} {
		t.Run(target, func(t *testing.T) {
			cmd := exec.Command("make", "-n", target, "AGENTS=claudecode,codex", "PLATFORMS_INCLUDE=telegram", "WITH_WEB=0", "EXCLUDE=")
			cmd.Dir = "../.."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make: %v: %s", err, out)
			}
			output := string(out)
			if strings.Contains(output, "npm ") {
				t.Fatalf("unexpected frontend build: %s", output)
			}
			var tags string
			for _, line := range strings.Split(output, "\n") {
				if !strings.HasPrefix(line, "go build ") {
					continue
				}
				if strings.Count(line, "-tags") != 1 {
					t.Fatalf("conflicting tag flags: %s", line)
				}
				_, tail, ok := strings.Cut(line, "-tags '")
				if !ok {
					t.Fatalf("missing tags: %s", line)
				}
				tags, _, _ = strings.Cut(tail, "'")
			}
			if tags == "" {
				t.Fatalf("no build command: %s", output)
			}
			ctx := build.Default
			ctx.BuildTags = strings.Fields(tags)
			files, err := filepath.Glob("plugin_*.go")
			if err != nil {
				t.Fatal(err)
			}
			var included []string
			for _, file := range files {
				match, err := ctx.MatchFile(".", file)
				if err != nil {
					t.Fatal(err)
				}
				if match {
					included = append(included, file)
				}
			}
			sort.Strings(included)
			want := []string{"plugin_agent_claudecode.go", "plugin_agent_codex.go", "plugin_platform_telegram.go"}
			if !reflect.DeepEqual(included, want) {
				t.Fatalf("included = %v; want %v", included, want)
			}
		})
	}
}
