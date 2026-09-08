//go:build !full_plugins || no_yuanbao

package main

import (
	"fmt"
	"os"

	"github.com/chenhg5/cc-connect/core"
)

func runYuanbao(args []string) {
	fmt.Fprintln(os.Stderr, core.NewI18n(core.LangEnglish).Tf(core.MsgBuildSupportExcluded, "Yuanbao"))
	os.Exit(1)
}
