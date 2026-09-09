//go:build !windows

package main

import (
	"github.com/chenhg5/cc-connect/core"
	"os"
	"testing"
)

func TestIsolatedAPIProbeChecksRoutingAndLiveSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("CC_DATA_DIR", dir)
	t.Setenv("CC_PROJECT", "p")
	t.Setenv("CC_SESSION_KEY", "")
	if probeIsolatedAPI() == nil {
		t.Fatal("missing route accepted")
	}
	t.Setenv("CC_SESSION_KEY", "shared-request:probe")
	if probeIsolatedAPI() == nil {
		t.Fatal("missing socket accepted")
	}
	api, err := core.NewAPIServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	api.Start()
	defer api.Stop()
	if err := probeIsolatedAPI(); err != nil {
		t.Fatal(err)
	}
}
