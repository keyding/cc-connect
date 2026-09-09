//go:build !windows

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// Probe uses the same sudo/environment boundary as Agent startup. It neither
// sends a message nor prints the session list returned by the read-only API.
func checkIsolatedAPI(ctx context.Context, username, dataDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	opts := core.SpawnOptions{RunAsUser: username}
	cmd := core.BuildSpawnCommand(ctx, opts, exe, "doctor", "_api-probe")
	cmd.Env = core.FilterEnvForSpawn(core.MergeEnv(os.Environ(), []string{"CC_DATA_DIR=" + dataDir, "CC_PROJECT=doctor-probe", "CC_SESSION_KEY=doctor-probe"}), opts)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("target-user probe failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func probeIsolatedAPI() error {
	for _, key := range []string{"CC_DATA_DIR", "CC_PROJECT", "CC_SESSION_KEY"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return fmt.Errorf("missing routing environment %s", key)
		}
	}
	path := filepath.Join(os.Getenv("CC_DATA_DIR"), "run", "api.sock")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get("http://localhost/sessions")
	if err != nil {
		return fmt.Errorf("connect to %s: %w; check ancestor traversal, api_socket_group and restart the service", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API access returned HTTP %d", resp.StatusCode)
	}
	return nil
}
