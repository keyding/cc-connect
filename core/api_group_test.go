//go:build !windows

package core

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestAPISocketGroupReappliedAfterRestart(t *testing.T) {
	group, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "cca-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for i := 0; i < 2; i++ {
		api, err := NewAPIServerWithGroup(dir, group.Name)
		if err != nil {
			t.Fatal(err)
		}
		for path, mode := range map[string]os.FileMode{filepath.Join(dir, "run"): 0750, api.SocketPath(): 0660} {
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != mode || int(st.Sys().(*syscall.Stat_t).Gid) != os.Getgid() {
				t.Fatalf("wrong access: %s %v", path, st.Mode())
			}
		}
		api.listener.Close()
	}
	api, err := NewAPIServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer api.listener.Close()
	st, _ := os.Stat(api.SocketPath())
	if st.Mode().Perm() != 0600 {
		t.Fatal("default is not private")
	}
	if _, err := NewAPIServerWithGroup(t.TempDir(), "cc-connect-nonexistent-group-123456"); err == nil {
		t.Fatal("unknown group accepted")
	}
}
