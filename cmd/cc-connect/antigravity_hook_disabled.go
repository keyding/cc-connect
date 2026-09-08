//go:build !full_plugins || no_antigravity

package main

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/chenhg5/cc-connect/core"
)

func runAntigravityPermissionHook() {
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{
		"decision": "deny",
		"reason":   core.NewI18n(core.LangEnglish).Tf(core.MsgBuildSupportExcluded, "Antigravity"),
	}); err != nil {
		slog.Error("write permission hook denial", "error", err)
	}
}
