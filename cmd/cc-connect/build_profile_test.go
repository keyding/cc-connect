package main

import (
	"os"
	"path/filepath"
	"testing"

	ccconnect "github.com/chenhg5/cc-connect"
)

func TestBootstrapConfig_UsesTelegramExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := bootstrapConfig(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != ccconnect.ConfigExampleTOML {
		t.Fatal("bootstrap config differs from the supported example")
	}
}
